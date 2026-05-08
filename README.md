# heic2jpg

一个用 Go 写的批量 HEIC/HEIF 转 JPG 工具。

特点：

- 直接运行后输入文件夹路径即可使用
- 只需要输入 1-10 转换等级
- 自动遍历 `.heic` / `.heif`，大小写不敏感
- 默认递归扫描子目录
- 自动按 CPU 核心数多线程转换
- 转换时显示单行进度条，不再逐个文件刷屏
- 输出到原文件所在目录
- 自动保留原文件名，仅后缀改成 `.jpg`
- 默认不覆盖已有 JPG
- 转换完成后可确认是否删除原始 HEIC/HEIF 文件

## 依赖

本工具本身用 Go 编写，但 HEIC 解码依赖系统转换器。请安装以下任意一种：

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

如果能看到版本号，就可以运行 `heic2jpg.exe`。

如果忘记勾选 PATH，程序也会自动尝试查找：

```text
C:\Program Files\ImageMagick-*\magick.exe
C:\Program Files (x86)\ImageMagick-*\magick.exe
```

程序会优先使用 `magick`，找不到时自动尝试 `heif-convert`，macOS 上还会自动尝试系统自带的 `sips`。

## 编译

```bash
go build -o heic2jpg .
```

## 使用

### 方式一：直接运行

```bash
./heic2jpg
```

程序会提示：

```text
请输入要转换的文件夹或文件路径，然后按回车。
路径:

请输入转换等级 1-10，然后按回车。
1 = 文件更小，10 = 最高画质/近似无损
等级 [10]:

是否在转换全部完成后自动删除原始 HEIC/HEIF 文件？
完成后自动删除原文件？y/N [N]:
```

把文件夹路径粘贴进去即可，也可以把文件夹/文件直接拖到窗口里，再按回车。

等级建议：

- `10`：最高画质/近似无损，适合照片归档
- `8-9`：高画质，体积比 10 小
- `5-7`：日常分享，体积更小
- `1-4`：优先压缩体积

### 方式二：命令行参数

```bash
# 转换目录，默认递归扫描，等级默认 10
./heic2jpg -input /path/to/photos

# 也可以直接把路径作为第一个参数
./heic2jpg /path/to/photos

# 转换单个文件
./heic2jpg /path/to/IMG_0001.HEIC

# 设置转换等级
./heic2jpg -input /path/to/photos -level 8

# 覆盖已有 jpg
./heic2jpg -input /path/to/photos -overwrite

# 转换完成后自动删除本次成功转换的原始 HEIC/HEIF 文件
./heic2jpg -input /path/to/photos -delete-original

# 只扫描当前目录，不递归子目录
./heic2jpg -input /path/to/photos -recursive=false

# 手动指定并发线程数；默认自动使用 CPU 核心数
./heic2jpg -input /path/to/photos -workers 4
```

## 进度显示

转换时会显示单行进度条：

```text
[████████████░░░░░░░░░░░░░░░░░░]  40.00%  20/50  成功:18 跳过:2 失败:0  耗时:8s
```

如果有失败，程序会在全部任务结束后统一打印失败详情。

## 输出规则

输出文件固定在原文件所在目录，文件名保持不变，只把扩展名改为 `.jpg`：

```text
IMG_001.HEIC -> IMG_001.jpg
IMG_002.heif -> IMG_002.jpg
```

如果目录里已经有同名 JPG，默认跳过。跳过的文件不会删除原始 HEIC/HEIF。

需要覆盖时使用：

```bash
./heic2jpg -input /path/to/photos -overwrite
```


## 删除原始文件

默认流程更安全：先完成全部转换，显示统计结果和失败详情，然后让你确认是否删除原始 HEIC/HEIF 文件。

交互模式下，如果本次有成功转换的文件，结束后会提示：

```text
本次成功转换 N 个文件。请确认 JPG 数据没问题。
是否现在删除这些成功转换对应的原始 HEIC/HEIF 文件？
确认删除原文件？y/N:
```

输入 `y` 才会删除；直接回车会保留原文件。

如果你确定想在转换完成后自动删除，可以使用命令行参数：

```bash
./heic2jpg -input /path/to/photos -delete-original
```

安全规则：

- 转换过程中不会边转边删
- 只有本次 JPG 转换成功的文件，才会被加入待删除列表
- 转换失败不会删除
- 因已有 JPG 被跳过的文件不会删除
- 删除发生在全部转换和统计输出之后
