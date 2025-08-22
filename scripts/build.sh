#!/bin/bash

# HeadCNI 构建脚本
# 用于构建 HeadCNI daemon 并注入版本信息

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 默认值
VERSION=${VERSION:-"dev"}
BUILD_DATE=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_OS=${BUILD_OS:-$(go env GOOS)}
BUILD_ARCH=${BUILD_ARCH:-$(go env GOARCH)}
OUTPUT_DIR=${OUTPUT_DIR:-"bin"}

# 显示构建信息
echo -e "${GREEN}Building HeadCNI Daemon${NC}"
echo "Version: $VERSION"
echo "Build Date: $BUILD_DATE"
echo "Git Commit: $GIT_COMMIT"
echo "OS: $BUILD_OS"
echo "Arch: $BUILD_ARCH"
echo "Output: $OUTPUT_DIR"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

# 构建标志
LDFLAGS="-X github.com/binrclab/headcni/cmd/headcni-daemon/command.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/binrclab/headcni/cmd/headcni-daemon/command.BuildDate=$BUILD_DATE"
LDFLAGS="$LDFLAGS -X github.com/binrclab/headcni/cmd/headcni-daemon/command.GitCommit=$GIT_COMMIT"
LDFLAGS="$LDFLAGS -s -w" # 去除调试信息，减小二进制大小

# 构建二进制文件
echo -e "${YELLOW}Building binary...${NC}"
go build \
    -ldflags "$LDFLAGS" \
    -o "$OUTPUT_DIR/headcni-daemon" \
    ./cmd/headcni-daemon

# 检查构建是否成功
if [ $? -eq 0 ]; then
    echo -e "${GREEN}Build successful!${NC}"
    echo "Binary location: $OUTPUT_DIR/headcni-daemon"
    
    # 显示二进制信息
    echo -e "${YELLOW}Binary information:${NC}"
    file "$OUTPUT_DIR/headcni-daemon"
    ls -lh "$OUTPUT_DIR/headcni-daemon"
    
    # 测试版本命令
    echo -e "${YELLOW}Testing version command:${NC}"
    "$OUTPUT_DIR/headcni-daemon" version
else
    echo -e "${RED}Build failed!${NC}"
    exit 1
fi

# 可选：创建 Docker 镜像
if [ "$BUILD_DOCKER" = "true" ]; then
    echo -e "${YELLOW}Building Docker image...${NC}"
    docker build \
        --build-arg VERSION="$VERSION" \
        --build-arg BUILD_DATE="$BUILD_DATE" \
        --build-arg GIT_COMMIT="$GIT_COMMIT" \
        -t headcni-daemon:"$VERSION" \
        -f Dockerfile .
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}Docker image built successfully!${NC}"
        echo "Image: headcni-daemon:$VERSION"
    else
        echo -e "${RED}Docker build failed!${NC}"
        exit 1
    fi
fi

echo -e "${GREEN}Build completed successfully!${NC}" 