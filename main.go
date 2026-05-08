package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
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
)

type config struct {
	input     string
	recursive bool
	overwrite bool
	level     int
	workers   int
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
	jobCh := make(chan job)
	var wg sync.WaitGroup

	for i := 1; i <= workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobCh {
				if !cfg.overwrite {
					if _, err := os.Stat(j.dst); err == nil {
						log.Printf("[SKIP][%02d] %s -> %s 已存在", workerID, j.src, j.dst)
						skipCount.Add(1)
						continue
					}
				}

				log.Printf("[DO][%02d] %s -> %s", workerID, j.src, j.dst)
				if err := conv.run(j.src, j.dst, opt); err != nil {
					log.Printf("[FAIL][%02d] %s -> %v", workerID, j.src, err)
					failCount.Add(1)
					continue
				}
				okCount.Add(1)
			}
		}(i)
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()

	fmt.Println("------------------------------")
	fmt.Printf("总数: %d, 成功: %d, 跳过: %d, 失败: %d\n", len(jobs), okCount.Load(), skipCount.Load(), failCount.Load())
	if failCount.Load() > 0 {
		os.Exit(2)
	}
}

func parseFlags() (*config, error) {
	cfg := &config{}
	flag.StringVar(&cfg.input, "input", "", "输入文件或目录")
	flag.BoolVar(&cfg.recursive, "recursive", true, "输入为目录时是否递归扫描")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "是否覆盖已存在 jpg")
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

func detectConverter() (*converter, error) {
	if path, err := findMagick(); err == nil {
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

func outputPath(src string) string {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	return filepath.Join(filepath.Dir(src), base+".jpg")
}

func isHEIC(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".heic" || ext == ".heif"
}
