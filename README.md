# heic2jpg

一个用 Go 写的批量 HEIC/HEIF 转 JPG 工具。

特点：

- 支持输入单个文件或目录
- 输入目录时默认递归遍历
- 自动识别 `.heic` / `.heif`，大小写不敏感
- 输出最高画质 JPG
- 默认不覆盖已有文件
- 可指定输出目录，并保留原目录结构

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

```bash
brew install imagemagick
```

或：

```bash
brew install libheif
```

程序会优先使用 `magick`，找不到时自动尝试 `heif-convert`。

## 编译

```bash
go build -o heic2jpg .
```

## 使用

```bash
# 转换目录，默认递归扫描
./heic2jpg -input /path/to/photos

# 也可以直接把路径作为第一个参数
./heic2jpg /path/to/photos

# 转换单个文件
./heic2jpg /path/to/IMG_0001.HEIC

# 输出到指定目录，保留原目录结构
./heic2jpg -input /path/to/photos -out /path/to/jpg-output

# 覆盖已有 jpg
./heic2jpg -input /path/to/photos -overwrite

# 只扫描当前目录，不递归子目录
./heic2jpg -input /path/to/photos -recursive=false
```

## 示例

```text
IMG_001.HEIC -> IMG_001.jpg
IMG_002.heif -> IMG_002.jpg
```

如果指定输出目录：

```bash
./heic2jpg -input ./photos -out ./jpg-output
```

目录结构会被保留：

```text
photos/a/IMG_001.HEIC -> jpg-output/a/IMG_001.jpg
```
