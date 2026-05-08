package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type config struct {
	input     string
	outputDir string
	recursive bool
	overwrite bool
	keepName  bool
}

type converter struct {
	name string
	run  func(src, dst string) error
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("参数错误: %v", err)
	}

	conv, err := detectConverter()
	if err != nil {
		log.Fatalf("未找到可用 HEIC 转换器: %v\n\n请先安装以下任意一种工具：\n1. ImageMagick（magick）\n2. libheif-tools（heif-convert）", err)
	}

	files, rootBase, err := collectHEICFiles(cfg.input, cfg.recursive)
	if err != nil {
		log.Fatalf("扫描输入失败: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("没有找到 HEIC/HEIF 文件: %s", cfg.input)
	}

	if cfg.outputDir != "" {
		if err := os.MkdirAll(cfg.outputDir, 0o755); err != nil {
			log.Fatalf("创建输出目录失败: %v", err)
		}
	}

	var okCount, skipCount, failCount int
	for _, src := range files {
		dst, err := buildOutputPath(src, rootBase, cfg)
		if err != nil {
			log.Printf("[FAIL] %s -> 构造输出路径失败: %v", src, err)
			failCount++
			continue
		}

		if !cfg.overwrite {
			if _, err := os.Stat(dst); err == nil {
				log.Printf("[SKIP] %s -> %s 已存在", src, dst)
				skipCount++
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			log.Printf("[FAIL] %s -> 创建目录失败: %v", src, err)
			failCount++
			continue
		}

		log.Printf("[DO]   %s -> %s", src, dst)
		if err := conv.run(src, dst); err != nil {
			log.Printf("[FAIL] %s -> %v", src, err)
			failCount++
			continue
		}
		okCount++
	}

	fmt.Println("------------------------------")
	fmt.Printf("转换器: %s\n", conv.name)
	fmt.Printf("总数: %d, 成功: %d, 跳过: %d, 失败: %d\n", len(files), okCount, skipCount, failCount)
	if failCount > 0 {
		os.Exit(2)
	}
}

func parseFlags() (*config, error) {
	cfg := &config{}
	flag.StringVar(&cfg.input, "input", "", "输入文件或目录")
	flag.StringVar(&cfg.outputDir, "out", "", "输出目录；默认输出到源文件同目录")
	flag.BoolVar(&cfg.recursive, "recursive", true, "输入为目录时是否递归扫描")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "是否覆盖已存在 jpg")
	flag.BoolVar(&cfg.keepName, "keep-name", true, "保留原文件名，仅扩展名改为 .jpg")
	flag.Parse()

	if cfg.input == "" && flag.NArg() > 0 {
		cfg.input = flag.Arg(0)
	}
	if strings.TrimSpace(cfg.input) == "" {
		return nil, errors.New("请提供 -input <文件或目录>，也可以直接把路径作为第一个参数")
	}

	absInput, err := filepath.Abs(cfg.input)
	if err != nil {
		return nil, err
	}
	cfg.input = absInput

	if cfg.outputDir != "" {
		cfg.outputDir, err = filepath.Abs(cfg.outputDir)
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func detectConverter() (*converter, error) {
	if path, err := exec.LookPath("magick"); err == nil {
		return &converter{
			name: "ImageMagick: " + path,
			run: func(src, dst string) error {
				cmd := exec.Command("magick", src, "-auto-orient", "-strip", "-quality", "100", dst)
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
			run: func(src, dst string) error {
				cmd := exec.Command("heif-convert", src, dst)
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("heif-convert 转换失败: %v: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
		}, nil
	}

	return nil, errors.New("magick / heif-convert 都不可用")
}

func collectHEICFiles(input string, recursive bool) ([]string, string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, "", err
	}

	if !info.IsDir() {
		if isHEIC(input) {
			return []string{input}, filepath.Dir(input), nil
		}
		return nil, filepath.Dir(input), fmt.Errorf("输入文件不是 HEIC/HEIF: %s", input)
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
		return nil, input, err
	}
	sort.Strings(files)
	return files, input, nil
}

func buildOutputPath(src, rootBase string, cfg *config) (string, error) {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	if !cfg.keepName {
		base = sanitizeName(base)
	}
	fileName := base + ".jpg"

	if cfg.outputDir == "" {
		return filepath.Join(filepath.Dir(src), fileName), nil
	}

	rel, err := filepath.Rel(rootBase, filepath.Dir(src))
	if err != nil {
		return "", err
	}
	if rel == "." {
		return filepath.Join(cfg.outputDir, fileName), nil
	}
	return filepath.Join(cfg.outputDir, rel, fileName), nil
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		return "converted"
	}
	return name
}

func isHEIC(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".heic" || ext == ".heif"
}
