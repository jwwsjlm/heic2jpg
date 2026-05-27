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
	"sync"
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
	ctx        context.Context
	converting atomic.Bool
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
		Title:       appTitle(),
		Width:       1120,
		Height:      760,
		MinWidth:    480,
		MinHeight:   520,
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
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

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	wailsruntime.OnFileDrop(ctx, func(_ int, _ int, paths []string) {
		if len(paths) == 0 {
			return
		}
		wailsruntime.EventsEmit(ctx, "source:dropped", paths[0])
	})
}

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

func (a *App) ConverterStatus() ConverterStatus { return converterStatus() }

func (a *App) ValidateInput(input string) (string, error) {
	return validateInputPath(input)
}

func (a *App) StartConversion(req ConvertRequest) (*ConvertResult, error) {
	if !a.converting.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("已有转换任务正在运行，请等待完成")
	}
	defer a.converting.Store(false)

	if req.Level == 0 {
		req.Level = 10
	}
	if req.Workers == 0 {
		req.Workers = defaultWorkerCount()
	}
	cfg := &config{
		input:          req.Input,
		recursive:      req.Recursive,
		overwrite:      req.Overwrite,
		deleteOriginal: req.DeleteOriginal,
		level:          req.Level,
		workers:        req.Workers,
	}
	return runGUIConversion(cfg, func(p ConvertProgress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "conversion:progress", p)
		}
	})
}

func runGUIConversion(cfg *config, onProgress func(ConvertProgress)) (*ConvertResult, error) {
	started := time.Now()
	if cfg == nil {
		return nil, fmt.Errorf("缺少转换配置")
	}
	absInput, err := validateInputPath(cfg.input)
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
	cfg.workers = sanitizeWorkerCount(cfg.workers)

	opt := levelToJPGOptions(cfg.level)
	conv, err := detectConverter()
	if err != nil {
		return nil, errors.New(converterInstallHelp(err))
	}

	var foundCount, okCount, skipCount, failCount atomic.Int64
	successes := newSuccessList()
	failures := newFailureList()
	jobCh := make(chan job, cfg.workers*2)
	var wg sync.WaitGroup

	if onProgress == nil {
		onProgress = func(ConvertProgress) {}
	}
	emit := newProgressEmitter(onProgress, &foundCount, &okCount, &skipCount, &failCount)
	emit("准备转换", true)

	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if !cfg.overwrite {
					if _, err := os.Stat(j.dst); err == nil {
						skipCount.Add(1)
						emit("已跳过存在的 JPG", false)
						continue
					}
				}

				if err := conv.run(j.src, j.dst, opt); err != nil {
					failCount.Add(1)
					failures.add(fmt.Sprintf("%s -> %v", j.src, err))
					emit("转换失败", true)
					continue
				}
				if err := preserveFileTimes(j.src, j.dst); err != nil {
					failCount.Add(1)
					failures.add(fmt.Sprintf("%s -> JPG 已生成，但同步文件修改时间失败: %v", j.src, err))
					emit("同步文件时间失败", true)
					continue
				}

				successes.add(j.src)
				okCount.Add(1)
				emit("转换中", false)
			}
		}()
	}

	if err := streamHEICJobs(cfg.input, cfg.recursive, func(j job) {
		foundCount.Add(1)
		emit("扫描到 HEIC/HEIF", false)
		jobCh <- j
	}); err != nil {
		close(jobCh)
		wg.Wait()
		return nil, fmt.Errorf("扫描输入失败: %w", err)
	}
	close(jobCh)
	wg.Wait()

	if foundCount.Load() == 0 {
		return nil, fmt.Errorf("没有找到 HEIC/HEIF 文件: %s", cfg.input)
	}

	res := &ConvertResult{
		Converter: conv.name,
		Found:     foundCount.Load(),
		Success:   okCount.Load(),
		Skipped:   skipCount.Load(),
		Failed:    failCount.Load(),
		Failures:  failures.items(),
		Duration:  time.Since(started).Round(time.Second).String(),
	}
	if cfg.deleteOriginal && okCount.Load() > 0 {
		emit("正在备份原图", true)
		moved, moveFailures, backupDir := backupOriginals(successes.items(), cfg.input)
		res.Moved, res.MoveFailures, res.BackupDir = moved, moveFailures, backupDir
	}
	emit("转换完成", true)
	if res.Failed > 0 || len(res.MoveFailures) > 0 {
		return res, fmt.Errorf("转换完成，但有 %d 个失败", res.Failed+int64(len(res.MoveFailures)))
	}
	return res, nil
}

func newProgressEmitter(onProgress func(ConvertProgress), foundCount, okCount, skipCount, failCount *atomic.Int64) func(string, bool) {
	var mu sync.Mutex
	lastEmit := time.Time{}
	return func(message string, force bool) {
		found, ok, skipped, failed := foundCount.Load(), okCount.Load(), skipCount.Load(), failCount.Load()
		done := ok + skipped + failed
		percent := 0
		if found > 0 {
			percent = int(done * 100 / found)
		}

		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		if !force && now.Sub(lastEmit) < 200*time.Millisecond {
			return
		}
		lastEmit = now

		onProgress(ConvertProgress{
			Found:   found,
			Done:    done,
			Success: ok,
			Skipped: skipped,
			Failed:  failed,
			Percent: percent,
			Message: message,
		})
	}
}

// RuntimeInfo is shown in the GUI help panel.
func (a *App) RuntimeInfo() map[string]string {
	return map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "version": appVersion(), "title": appTitle()}
}

func validateInputPath(input string) (string, error) {
	input = cleanInputPath(input)
	if input == "" {
		return "", fmt.Errorf("请选择 HEIC/HEIF 文件或文件夹")
	}
	if !looksLikeFilesystemPath(input) {
		return "", fmt.Errorf("接收到的来源路径不完整，请重新选择文件或文件夹")
	}
	absInput, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(absInput); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("所选路径不存在，请重新选择：%s", absInput)
		}
		return "", err
	}
	return absInput, nil
}
func appTitle() string {
	return "HEIC 转 JPG " + displayVersion()
}
