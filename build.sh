#!/bin/bash
# 跨平台交叉编译脚本 (Linux/macOS)
# 用法: ./build.sh

set -e

APP_NAME="go-remote-terminal"
VERSION=${VERSION:-"1.0.0"}
BUILD_DIR="build"
LDFLAGS="-s -w -X main.Version=${VERSION}"

echo "=== Building ${APP_NAME} v${VERSION} ==="
echo ""

# 创建构建目录
mkdir -p ${BUILD_DIR}

# 清理旧构建
rm -f ${BUILD_DIR}/${APP_NAME}-*

# 构建函数
build() {
    local goos=$1
    local goarch=$2
    local output=$3

    echo "Building for ${goos}/${goarch}..."
    CGO_ENABLED=0 GOOS=${goos} GOARCH=${goarch} go build \
        -ldflags "${LDFLAGS}" \
        -o ${BUILD_DIR}/${output} \
        .

    if [ $? -eq 0 ]; then
        echo "  ✓ ${output}"
    else
        echo "  ✗ ${output} FAILED"
        exit 1
    fi
}

# Windows amd64
build windows amd64 "${APP_NAME}-windows-amd64.exe"

# Linux amd64
build linux amd64 "${APP_NAME}-linux-amd64"

# macOS Intel
build darwin amd64 "${APP_NAME}-darwin-amd64"

# macOS Apple Silicon
build darwin arm64 "${APP_NAME}-darwin-arm64"

echo ""
echo "=== Build Complete ==="
ls -lh ${BUILD_DIR}/
