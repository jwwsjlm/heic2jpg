package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type config struct {
	input          string
	recursive      bool
	overwrite      bool
	deleteOriginal bool
	level          int
	workers        int
}

type jpgOptions struct {
	quality     int
	keepMeta    bool
	progressive bool
}

type converter struct {
	name string
	run  func(src, dst string, opt jpgOptions) error
}

type job struct {
	src string
	dst string
}

type successList struct {
	mu   sync.Mutex
	list []string
}

func newSuccessList() *successList {
	return &successList{}
}

func (s *successList) add(path string) {
	s.mu.Lock()
	s.list = append(s.list, path)
	s.mu.Unlock()
}

func (s *successList) items() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}

var stdinReader = bufio.NewReader(os.Stdin)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("参数错误: %v", err)
	}

	opt := levelToJPGOptions(cfg.level)

	conv, err := detectConverter()
	if err != nil {
		log.Fatalf("%s", converterInstallHelp(err))
	}

	files, err := collectHEICFiles(cfg.input, cfg.recursive)
	if err != nil {
		log.Fatalf("扫描输入失败: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("没有找到 HEIC/HEIF 文件: %s", cfg.input)
	}

	jobs := make([]job, 0, len(files))
	for _, src := range files {
		jobs = append(jobs, job{src: src, dst: outputPath(src)})
	}

	existingCount := countExistingOutputs(jobs)
	fmt.Println("------------------------------")
	fmt.Printf("预检查: 找到 HEIC/HEIF %d 个，待输出 JPG %d 个，已存在会跳过 %d 个\n", len(files), len(jobs), existingCount)
	if existingCount > 0 && !cfg.overwrite {
		fmt.Println("提示: 如果想覆盖已有 JPG，请使用 -overwrite")
	}

	workerCount := cfg.workers
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}

	fmt.Println("------------------------------")
	fmt.Printf("转换器: %s\n", conv.name)
	fmt.Printf("等级: %d/10 -> JPG quality=%d, keep-meta=%v, progressive=%v\n", cfg.level, opt.quality, opt.keepMeta, opt.progressive)
	fmt.Printf("并发线程: %d，任务数: %d\n", workerCount, len(jobs))
	fmt.Println("输出规则: 原目录，同文件名，后缀改为 .jpg")
	fmt.Println("------------------------------")

	var okCount, skipCount, failCount atomic.Int64
	progress := newProgressBar(len(jobs))
	successes := newSuccessList()
	failures := newFailureList()
	jobCh := make(chan job)
	var wg sync.WaitGroup

	progress.render(okCount.Load(), skipCount.Load(), failCount.Load())

	for i := 1; i <= workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobCh {
				if !cfg.overwrite {
					if _, err := os.Stat(j.dst); err == nil {
						skipCount.Add(1)
						progress.add(okCount.Load(), skipCount.Load(), failCount.Load())
						continue
					}
				}

				if err := conv.run(j.src, j.dst, opt); err != nil {
					failCount.Add(1)
					failures.add(fmt.Sprintf("%s -> %v", j.src, err))
					progress.add(okCount.Load(), skipCount.Load(), failCount.Load())
					continue
				}
				successes.add(j.src)
				okCount.Add(1)
				progress.add(okCount.Load(), skipCount.Load(), failCount.Load())
			}
		}(i)
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()
	progress.finish(okCount.Load(), skipCount.Load(), failCount.Load())

	fmt.Println("------------------------------")
	fmt.Printf("总数: %d, 成功: %d, 跳过: %d, 失败: %d\n", len(jobs), okCount.Load(), skipCount.Load(), failCount.Load())
	if failCount.Load() > 0 {
		fmt.Println()
		fmt.Println("失败详情：")
		failureItems := failures.items()
		for _, msg := range failureItems {
			fmt.Println("- " + msg)
		}
		if logPath, err := writeFailureLog(failureItems); err == nil && logPath != "" {
			fmt.Printf("失败日志: %s\n", logPath)
		} else if err != nil {
			fmt.Printf("写入失败日志失败: %v\n", err)
		}
	}

	deleteRequested := cfg.deleteOriginal
	if !deleteRequested && okCount.Load() > 0 && isInteractiveTerminal() {
		answer, err := promptDeleteAfterReview(okCount.Load())
		if err != nil {
			fmt.Printf("读取删除确认失败，已保留原始文件: %v\n", err)
		} else {
			deleteRequested = answer
		}
	}

	if deleteRequested {
		moved, moveFailures, backupDir := backupOriginals(successes.items(), cfg.input)
		fmt.Println("------------------------------")
		fmt.Printf("原始文件已移动到备份目录: %s\n", backupDir)
		fmt.Printf("移动完成: 已移动 %d 个，失败 %d 个\n", moved, len(moveFailures))
		if len(moveFailures) > 0 {
			fmt.Println()
			fmt.Println("移动失败详情：")
			for _, msg := range moveFailures {
				fmt.Println("- " + msg)
			}
			os.Exit(2)
		}
	}

	if failCount.Load() > 0 {
		os.Exit(2)
	}
}

type progressBar struct {
	total   int64
	current int64
	start   time.Time
	mu      sync.Mutex
}

func newProgressBar(total int) *progressBar {
	return &progressBar{total: int64(total), start: time.Now()}
}

func (p *progressBar) add(ok, skip, fail int64) {
	p.mu.Lock()
	p.current++
	p.renderLocked(ok, skip, fail)
	p.mu.Unlock()
}

func (p *progressBar) render(ok, skip, fail int64) {
	p.mu.Lock()
	p.renderLocked(ok, skip, fail)
	p.mu.Unlock()
}

func (p *progressBar) finish(ok, skip, fail int64) {
	p.mu.Lock()
	p.current = p.total
	p.renderLocked(ok, skip, fail)
	fmt.Println()
	p.mu.Unlock()
}

func (p *progressBar) renderLocked(ok, skip, fail int64) {
	width := int64(30)
	filled := int64(0)
	percent := float64(100)
	if p.total > 0 {
		filled = p.current * width / p.total
		percent = float64(p.current) * 100 / float64(p.total)
	}
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", int(filled)) + strings.Repeat("░", int(width-filled))
	elapsed := time.Since(p.start).Round(time.Second)
	fmt.Printf("\r[%s] %6.2f%%  %d/%d  成功:%d 跳过:%d 失败:%d  耗时:%s", bar, percent, p.current, p.total, ok, skip, fail, elapsed)
}

type failureList struct {
	mu   sync.Mutex
	list []string
}

func newFailureList() *failureList {
	return &failureList{}
}

func (f *failureList) add(msg string) {
	f.mu.Lock()
	f.list = append(f.list, msg)
	f.mu.Unlock()
}

func (f *failureList) items() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.list))
	copy(out, f.list)
	return out
}

func parseFlags() (*config, error) {
	cfg := &config{}
	flag.StringVar(&cfg.input, "input", "", "输入文件或目录")
	flag.BoolVar(&cfg.recursive, "recursive", true, "输入为目录时是否递归扫描")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "是否覆盖已存在 jpg")
	flag.BoolVar(&cfg.deleteOriginal, "delete-original", false, "转换完成后将本次成功转换的原始 HEIC/HEIF 移动到备份目录；默认不移动")
	flag.IntVar(&cfg.level, "level", 10, "转换等级，1-10；10 为最高质量/近似无损")
	flag.IntVar(&cfg.workers, "workers", 0, "并发线程数；默认 0 表示自动使用 CPU 核心数")
	flag.Parse()

	if cfg.input == "" && flag.NArg() > 0 {
		cfg.input = flag.Arg(0)
	}

	interactive := strings.TrimSpace(cfg.input) == ""
	if interactive {
		input, err := promptInputPath()
		if err != nil {
			return nil, err
		}
		cfg.input = input

		level, err := promptLevel(cfg.level)
		if err != nil {
			return nil, err
		}
		cfg.level = level

		deleteOriginal, err := promptDeleteOriginal(cfg.deleteOriginal)
		if err != nil {
			return nil, err
		}
		cfg.deleteOriginal = deleteOriginal
	}

	absInput, err := filepath.Abs(cfg.input)
	if err != nil {
		return nil, err
	}
	cfg.input = absInput

	if cfg.level < 1 || cfg.level > 10 {
		return nil, fmt.Errorf("转换等级必须在 1-10 之间，当前是 %d", cfg.level)
	}
	if cfg.workers < 0 {
		return nil, fmt.Errorf("并发线程数不能小于 0，当前是 %d", cfg.workers)
	}

	return cfg, nil
}

func promptInputPath() (string, error) {
	fmt.Println("HEIC/HEIF 批量转 JPG 工具")
	fmt.Println("--------------------------------")
	fmt.Println("请输入要转换的文件夹或文件路径，然后按回车。")
	fmt.Println("提示：可以直接拖拽文件夹/文件到这个窗口里。")

	input, err := promptLine("路径: ")
	if err != nil {
		return "", err
	}

	input = cleanInputPath(input)
	if input == "" {
		return "", errors.New("没有输入路径")
	}
	return input, nil
}

func promptLevel(defaultLevel int) (int, error) {
	fmt.Println()
	fmt.Println("请输入转换等级 1-10，然后按回车。")
	fmt.Println("1 = 文件更小，10 = 最高画质/近似无损")

	text, err := promptLine(fmt.Sprintf("等级 [%d]: ", defaultLevel))
	if err != nil {
		return 0, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultLevel, nil
	}

	var level int
	if _, err := fmt.Sscanf(text, "%d", &level); err != nil {
		return 0, fmt.Errorf("等级不是有效数字: %s", text)
	}
	return level, nil
}

func promptDeleteOriginal(defaultValue bool) (bool, error) {
	fmt.Println()
	fmt.Println("是否在转换全部完成后自动移动原始 HEIC/HEIF 到备份目录？")
	fmt.Println("建议直接回车选 N，等转换完成、确认数据没问题后再手动确认移动。")
	fmt.Println("注意：只有本次成功转换的文件才会移动；失败或跳过的文件不会移动。")

	defaultText := "N"
	if defaultValue {
		defaultText = "Y"
	}
	text, err := promptLine(fmt.Sprintf("完成后自动移动原文件到备份目录？y/N [%s]: ", defaultText))
	if err != nil {
		return false, err
	}
	return parseYesNo(text, defaultValue), nil
}

func promptDeleteAfterReview(successCount int64) (bool, error) {
	fmt.Println()
	fmt.Printf("本次成功转换 %d 个文件。请确认 JPG 数据没问题。\n", successCount)
	fmt.Println("是否现在把这些成功转换对应的原始 HEIC/HEIF 文件移动到备份目录？")
	fmt.Println("输入 y 移动；直接回车保留原文件。")
	text, err := promptLine("确认移动原文件到备份目录？y/N: ")
	if err != nil {
		return false, err
	}
	return parseYesNo(text, false), nil
}

func backupOriginals(paths []string, input string) (int, []string, string) {
	base := backupBaseDir(input)
	backupDir := filepath.Join(base, "_heic_original_backup_"+time.Now().Format("20060102-150405"))
	moved := 0
	var failures []string

	for _, src := range paths {
		rel, err := filepath.Rel(base, src)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(src)
		}
		dst := uniquePath(filepath.Join(backupDir, rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			failures = append(failures, fmt.Sprintf("%s -> 创建备份目录失败: %v", src, err))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			if copyErr := copyFile(src, dst); copyErr != nil {
				failures = append(failures, fmt.Sprintf("%s -> 移动到备份目录失败: %v", src, err))
				continue
			}
			if removeErr := os.Remove(src); removeErr != nil {
				failures = append(failures, fmt.Sprintf("%s -> 已复制到备份目录，但删除原位置失败: %v", src, removeErr))
				continue
			}
		}
		moved++
	}
	return moved, failures, backupDir
}

func backupBaseDir(input string) string {
	info, err := os.Stat(input)
	if err == nil && !info.IsDir() {
		return filepath.Dir(input)
	}
	return input
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func parseYesNo(input string, defaultValue bool) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	switch input {
	case "y", "yes", "是", "对", "1", "true":
		return true
	case "n", "no", "否", "不", "0", "false":
		return false
	default:
		return defaultValue
	}
}

func promptLine(label string) (string, error) {
	fmt.Print(label)
	return stdinReader.ReadString('\n')
}

func cleanInputPath(input string) string {
	input = strings.TrimSpace(input)
	input = strings.Trim(input, "\"'")
	// Some terminals escape spaces when users drag paths in, e.g. /a/b/My\ Photos.
	input = strings.ReplaceAll(input, `\ `, " ")
	return input
}

func levelToJPGOptions(level int) jpgOptions {
	// 1-10 映射到常用 JPG 质量区间。
	// 10 使用 quality=100，并保留元数据，作为最高画质/近似无损输出。
	qualityMap := map[int]int{
		1:  55,
		2:  65,
		3:  72,
		4:  78,
		5:  84,
		6:  88,
		7:  92,
		8:  95,
		9:  98,
		10: 100,
	}
	return jpgOptions{
		quality:     qualityMap[level],
		keepMeta:    level == 10,
		progressive: level <= 8,
	}
}

func findMagick() (string, error) {
	if path, err := exec.LookPath("magick"); err == nil {
		return path, nil
	}

	// Windows installer commonly places magick.exe under these directories.
	// This helps users who installed ImageMagick but did not add it to PATH.
	patterns := []string{
		`C:\Program Files\ImageMagick-*\magick.exe`,
		`C:\Program Files (x86)\ImageMagick-*\magick.exe`,
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1], nil
		}
	}

	return "", errors.New("magick not found")
}

func checkMagickHEICSupport(path string) error {
	cmd := exec.Command(path, "-list", "format")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ImageMagick 可执行，但检测格式支持失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	upper := strings.ToUpper(string(out))
	if !strings.Contains(upper, "HEIC") && !strings.Contains(upper, "HEIF") {
		return errors.New("ImageMagick 已安装，但当前版本没有检测到 HEIC/HEIF 支持")
	}
	return nil
}

func detectConverter() (*converter, error) {
	if path, err := findMagick(); err == nil {
		if err := checkMagickHEICSupport(path); err != nil {
			return nil, err
		}
		return &converter{
			name: "ImageMagick: " + path,
			run: func(src, dst string, opt jpgOptions) error {
				args := []string{src, "-auto-orient"}
				if !opt.keepMeta {
					args = append(args, "-strip")
				}
				if opt.progressive {
					args = append(args, "-interlace", "JPEG")
				}
				args = append(args, "-quality", fmt.Sprintf("%d", opt.quality), dst)
				cmd := exec.Command(path, args...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("magick 转换失败: %v: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
		}, nil
	}

	if path, err := exec.LookPath("heif-convert"); err == nil {
		return &converter{
			name: "libheif-tools: " + path,
			run: func(src, dst string, opt jpgOptions) error {
				cmd := exec.Command("heif-convert", "-q", fmt.Sprintf("%d", opt.quality), src, dst)
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("heif-convert 转换失败: %v: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
		}, nil
	}

	if path, err := exec.LookPath("sips"); err == nil {
		return &converter{
			name: "macOS sips: " + path,
			run: func(src, dst string, opt jpgOptions) error {
				cmd := exec.Command("sips", "-s", "format", "jpeg", "-s", "formatOptions", fmt.Sprintf("%d", opt.quality), src, "--out", dst)
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("sips 转换失败: %v: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
		}, nil
	}

	return nil, errors.New("magick / heif-convert / sips 都不可用")
}

func converterInstallHelp(err error) string {
	return fmt.Sprintf(`未找到可用 HEIC 转换器: %v

请按你的系统安装以下任意一种：

Ubuntu / Debian:
  sudo apt update && sudo apt install -y imagemagick

或者：
  sudo apt update && sudo apt install -y libheif-examples

macOS:
  通常系统自带 sips，不需要安装。
  如果仍然报错，可以安装 ImageMagick：
  brew install imagemagick

Windows:
  推荐安装 ImageMagick：
  https://imagemagick.org/script/download.php#windows

  安装时建议勾选：
  - Add application directory to your system path
  - Install HEIC/HEIF support 如果安装器里有这个选项

  如果忘记勾选 PATH，本程序也会自动尝试查找：
  C:\Program Files\ImageMagick-*\magick.exe
`, err)
}

func collectHEICFiles(input string, recursive bool) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if isHEIC(input) {
			return []string{input}, nil
		}
		return nil, fmt.Errorf("输入文件不是 HEIC/HEIF: %s", input)
	}

	var files []string
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != input && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if isHEIC(path) {
			files = append(files, path)
		}
		return nil
	}

	if err := filepath.WalkDir(input, walkFn); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func countExistingOutputs(jobs []job) int {
	count := 0
	for _, j := range jobs {
		if _, err := os.Stat(j.dst); err == nil {
			count++
		}
	}
	return count
}

func writeFailureLog(messages []string) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	name := "heic2jpg_failed_" + time.Now().Format("20060102-150405") + ".log"
	content := strings.Join(messages, "\n") + "\n"
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return name, nil
	}
	return abs, nil
}

func outputPath(src string) string {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	return filepath.Join(filepath.Dir(src), base+".jpg")
}

func isHEIC(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".heic" || ext == ".heif"
}
