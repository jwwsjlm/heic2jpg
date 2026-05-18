# heic2jpg

一个用 Go + Wails 写的 HEIC/HEIF 批量转 JPG 工具，同时保留命令行版本。GUI 标题栏和首页都会显示当前版本号，并支持把 HEIC 文件或文件夹拖到来源区域。

## 主要特点

- 提供桌面 GUI：选择文件/文件夹、拖拽文件/文件夹、调整画质、查看进度和结果更直观，并在窗口标题和界面里显示版本号
- 保留 CLI：老用户仍可用命令行批量转换
- 自动遍历 `.heic` / `.heif`，大小写不敏感
- 默认递归扫描子目录
- 自动按 CPU 核心数多线程转换
- 输出到原文件所在目录，同文件名，后缀改成 `.jpg`
- 自动保留原 HEIC/HEIF 的文件修改时间
- 默认不覆盖已有 JPG
- 可选择把本次成功转换的原始 HEIC/HEIF 移动到备份目录
- GUI 会显示当前系统可用的转换器状态；ImageMagick 会预检测 HEIC/HEIF 支持
- 失败时给出清晰错误信息，CLI 还会生成失败日志

## 转换器依赖

程序本身负责界面、扫描、并发和流程控制；HEIC 解码依赖系统转换器。请安装以下任意一种。

### Ubuntu / Debian

```bash
sudo apt install imagemagick
```

或：

```bash
sudo apt install libheif-examples
```

### macOS

macOS 通常自带 `sips`，可以直接转换 HEIC。

如果你的系统没有 `sips` 或转换失败，可以安装 ImageMagick：

```bash
brew install imagemagick
```

或：

```bash
brew install libheif
```

### Windows

推荐安装 ImageMagick：

<https://imagemagick.org/script/download.php#windows>

安装时建议勾选：

- `Add application directory to your system path`
- 如果安装器里有 HEIC/HEIF 支持选项，也一起勾选

装好后重新打开终端，运行：

```powershell
magick -version
```

如果能看到版本号，就可以使用本工具。

如果忘记勾选 PATH，程序也会自动尝试查找：

```text
C:\Program Files\ImageMagick-*\magick.exe
C:\Program Files (x86)\ImageMagick-*\magick.exe
```

程序会优先使用 `magick`，找不到时自动尝试 `heif-convert`，macOS 上还会自动尝试系统自带的 `sips`。

## 下载

GitHub Actions 会自动构建：

- GUI 桌面版：`heic2jpg-gui-v0.2.6-windows-amd64.zip`、`heic2jpg-gui-v0.2.6-macos-universal.tar.gz`、`heic2jpg-gui-v0.2.6-linux-amd64.tar.gz`
- CLI 命令行版：`heic2jpg-cli-v0.2.6-windows-amd64.zip`、`heic2jpg-cli-v0.2.6-linux-amd64.tar.gz`、`heic2jpg-cli-v0.2.6-darwin-amd64.tar.gz`、`heic2jpg-cli-v0.2.6-darwin-arm64.tar.gz`

推送 tag，例如 `v1.0.0`，会自动发布 GitHub Release。

## GUI 使用

1. 打开 `heic2jpg` 桌面程序。
2. 点击“选择 HEIC 文件”/“选择文件夹”，或直接把 HEIC 文件/文件夹拖到“选择来源”区域。
3. 选择画质等级：
   - `10`：最高画质 / 保留元数据，适合照片归档
   - `8-9`：高画质，体积比 10 小
   - `5-7`：日常分享
   - `1-4`：优先压缩体积
4. 按需开启：
   - 递归扫描子文件夹
   - 覆盖已存在 JPG
   - 成功后移动原 HEIC 到备份目录
5. 点击“开始转换”，等待进度完成。

GUI 默认更适合普通用户：不需要记命令，转换结果、失败数量和备份目录都会在界面里显示。

## CLI 使用

### 本地编译 CLI

```bash
go build -tags cli -ldflags "-X main.version=v0.2.6" -o heic2jpg-cli .
```

### 直接运行交互模式

```bash
./heic2jpg-cli
```

程序会提示输入文件或文件夹路径、转换等级，以及是否移动原文件。

### 命令行参数

```bash
# 转换目录，默认递归扫描，等级默认 10
./heic2jpg-cli -input /path/to/photos

# 也可以直接把路径作为第一个参数
./heic2jpg-cli /path/to/photos

# 转换单个文件
./heic2jpg-cli /path/to/IMG_0001.HEIC

# 设置转换等级
./heic2jpg-cli -input /path/to/photos -level 8

# 覆盖已有 jpg
./heic2jpg-cli -input /path/to/photos -overwrite

# 转换完成后移动本次成功转换的原始 HEIC/HEIF 文件到备份目录
./heic2jpg-cli -input /path/to/photos -delete-original

# 只扫描当前目录，不递归子目录
./heic2jpg-cli -input /path/to/photos -recursive=false

# 手动指定并发线程数；默认自动使用 min(CPU 核心数, 4)
./heic2jpg-cli -input /path/to/photos -workers 4
```

## 输出规则

输出文件固定在原文件所在目录，文件名保持不变，只把扩展名改为 `.jpg`：

```text
IMG_001.HEIC -> IMG_001.jpg
IMG_002.heif -> IMG_002.jpg
```

如果目录里已经有同名 JPG，默认跳过。跳过的文件不会移动原始 HEIC/HEIF。

## 原始文件备份

默认流程更安全：先完成全部转换，确认 JPG 没问题后，再决定是否移动原始 HEIC/HEIF。

`-delete-original` 或 GUI 中“成功后移动原 HEIC 到备份目录”不会永久删除文件，而是移动到软件所在目录下：

```text
_heic_original_backup_YYYYMMDD-HHMMSS
```

安全规则：

- 转换过程中不会边转边移动
- 只有本次 JPG 转换成功的文件才会移动
- 转换失败不会移动
- 因已有 JPG 被跳过的文件不会移动
- 备份目录固定创建在软件所在目录，方便统一管理

## 本地开发

```bash
# 安装前端依赖
npm --prefix frontend install

# 构建前端
npm --prefix frontend run build

# Go 测试
go test ./...

# 构建 CLI
go build -tags cli -ldflags "-X main.version=v0.2.6" -o heic2jpg-cli .

# 构建 GUI（需要 Wails CLI 和系统 WebView 依赖）
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
wails build -ldflags "-X main.version=v0.2.6"
```
