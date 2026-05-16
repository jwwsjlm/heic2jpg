//go:build !cli

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	ctx context.Context
}

type ConvertRequest struct {
	Input          string `json:"input"`
	Recursive      bool   `json:"recursive"`
	Overwrite      bool   `json:"overwrite"`
	DeleteOriginal bool   `json:"deleteOriginal"`
	Level          int    `json:"level"`
	Workers        int    `json:"workers"`
}

type ConvertProgress struct {
	Found   int64  `json:"found"`
	Done    int64  `json:"done"`
	Success int64  `json:"success"`
	Skipped int64  `json:"skipped"`
	Failed  int64  `json:"failed"`
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

type ConvertResult struct {
	Converter    string   `json:"converter"`
	Found        int64    `json:"found"`
	Success      int64    `json:"success"`
	Skipped      int64    `json:"skipped"`
	Failed       int64    `json:"failed"`
	Failures     []string `json:"failures"`
	BackupDir    string   `json:"backupDir"`
	Moved        int      `json:"moved"`
	MoveFailures []string `json:"moveFailures"`
	Duration     string   `json:"duration"`
}

func main() {
	if shouldRunCLI() {
		runCLI()
		return
	}
	app := &App{}
	err := wails.Run(&options.App{
		Title:       "HEIC 转 JPG",
		Width:       1120,
		Height:      760,
		MinWidth:    900,
		MinHeight:   640,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   app.startup,
		Bind:        []interface{}{app},
	})
	if err != nil {
		fmt.Println("启动 GUI 失败:", err)
		os.Exit(1)
	}
}

func shouldRunCLI() bool {
	if len(os.Args) <= 1 {
		return false
	}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--gui", "gui":
			return false
		case "--cli", "cli", "-input", "-recursive", "-overwrite", "-delete-original", "-level", "-workers", "-h", "--help":
			return true
		}
		if len(arg) > 0 && arg[0] != '-' {
			return true
		}
	}
	return false
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) SelectFile() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   "选择 HEIC/HEIF 文件",
		Filters: []wailsruntime.FileFilter{{DisplayName: "HEIC / HEIF 图片", Pattern: "*.heic;*.heif;*.HEIC;*.HEIF"}},
	})
}

func (a *App) SelectFolder() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择要批量转换的文件夹"})
}

func (a *App) DefaultWorkers() int { return defaultWorkerCount() }

func (a *App) StartConversion(req ConvertRequest) (*ConvertResult, error) {
	if req.Level == 0 {
		req.Level = 10
	}
	if req.Workers == 0 {
		req.Workers = defaultWorkerCount()
	}
	cfg := &config{input: req.Input, recursive: req.Recursive, overwrite: req.Overwrite, deleteOriginal: req.DeleteOriginal, level: req.Level, workers: req.Workers}
	result, err := runGUIConversion(cfg, func(p ConvertProgress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "conversion:progress", p)
		}
	})
	if a.ctx != nil {
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "conversion:error", err.Error())
		} else {
			wailsruntime.EventsEmit(a.ctx, "conversion:done", result)
		}
	}
	return result, err
}

func runGUIConversion(cfg *config, onProgress func(ConvertProgress)) (*ConvertResult, error) {
	started := time.Now()
	if cfg == nil {
		return nil, fmt.Errorf("缺少转换配置")
	}
	if cfg.input == "" {
		return nil, fmt.Errorf("请选择 HEIC/HEIF 文件或文件夹")
	}
	absInput, err := filepath.Abs(cfg.input)
	if err != nil {
		return nil, err
	}
	cfg.input = absInput
	if cfg.level < 1 || cfg.level > 10 {
		return nil, fmt.Errorf("转换等级必须在 1-10 之间")
	}
	if cfg.workers <= 0 {
		cfg.workers = defaultWorkerCount()
	}
	if cfg.workers < 1 {
		cfg.workers = 1
	}

	opt := levelToJPGOptions(cfg.level)
	conv, err := detectConverter()
	if err != nil {
		return nil, errors.New(converterInstallHelp(err))
	}

	var foundCount, okCount, skipCount, failCount atomic.Int64
	successes := newSuccessList()
	failures := newFailureList()
	jobCh := make(chan job)
	doneCh := make(chan struct{})

	emit := func(message string) {
		found, ok, skipped, failed := foundCount.Load(), okCount.Load(), skipCount.Load(), failCount.Load()
		done := ok + skipped + failed
		percent := 0
		if found > 0 {
			percent = int(done * 100 / found)
		}
		onProgress(ConvertProgress{Found: found, Done: done, Success: ok, Skipped: skipped, Failed: failed, Percent: percent, Message: message})
	}
	if onProgress == nil {
		onProgress = func(ConvertProgress) {}
	}
	emit("准备转换")

	for i := 0; i < cfg.workers; i++ {
		go func() {
			for j := range jobCh {
				if !cfg.overwrite {
					if _, err := os.Stat(j.dst); err == nil {
						skipCount.Add(1)
						emit("已跳过存在的 JPG")
						continue
					}
				}
				if err := conv.run(j.src, j.dst, opt); err != nil {
					failCount.Add(1)
					failures.add(fmt.Sprintf("%s -> %v", j.src, err))
					emit("转换失败")
					continue
				}
				if err := preserveFileTimes(j.src, j.dst); err != nil {
					failCount.Add(1)
					failures.add(fmt.Sprintf("%s -> JPG 已生成，但同步文件修改时间失败: %v", j.src, err))
					emit("同步文件时间失败")
					continue
				}
				successes.add(j.src)
				okCount.Add(1)
				emit("转换中")
			}
			doneCh <- struct{}{}
		}()
	}

	if err := streamHEICJobs(cfg.input, cfg.recursive, func(j job) { foundCount.Add(1); emit("扫描到 HEIC/HEIF"); jobCh <- j }); err != nil {
		close(jobCh)
		for i := 0; i < cfg.workers; i++ {
			<-doneCh
		}
		return nil, fmt.Errorf("扫描输入失败: %w", err)
	}
	close(jobCh)
	for i := 0; i < cfg.workers; i++ {
		<-doneCh
	}
	if foundCount.Load() == 0 {
		return nil, fmt.Errorf("没有找到 HEIC/HEIF 文件: %s", cfg.input)
	}

	res := &ConvertResult{Converter: conv.name, Found: foundCount.Load(), Success: okCount.Load(), Skipped: skipCount.Load(), Failed: failCount.Load(), Failures: failures.items(), Duration: time.Since(started).Round(time.Second).String()}
	if cfg.deleteOriginal && okCount.Load() > 0 {
		moved, moveFailures, backupDir := backupOriginals(successes.items(), cfg.input)
		res.Moved, res.MoveFailures, res.BackupDir = moved, moveFailures, backupDir
	}
	emit("转换完成")
	if res.Failed > 0 || len(res.MoveFailures) > 0 {
		return res, fmt.Errorf("转换完成，但有 %d 个失败", res.Failed+int64(len(res.MoveFailures)))
	}
	return res, nil
}

// RuntimeInfo is shown in the GUI help panel.
func (a *App) RuntimeInfo() map[string]string {
	return map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH}
}
