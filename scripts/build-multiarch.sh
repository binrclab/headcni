#!/bin/bash

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 项目信息
PROJECT_NAME="headcni"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
REGISTRY="binrclab"

# 支持的架构
ARCHITECTURES=("amd64" "arm64")

# 支持的基础镜像
BASE_IMAGES=(
    "alpine:3.19"
    "ubuntu:22.04"
    "centos:8"
    "fedora:38"
)

# 显示帮助信息
show_help() {
    echo "HeadCNI 多架构构建脚本"
    echo "========================"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -a, --arch <arch>     指定架构 (amd64, arm64, all)"
    echo "  -b, --base <image>    指定基础镜像"
    echo "  -p, --push            推送到镜像仓库"
    echo "  -h, --help            显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                      # 构建所有架构和基础镜像"
    echo "  $0 -a amd64            # 只构建 AMD64 架构"
    echo "  $0 -b alpine:3.19      # 只构建 Alpine 基础镜像"
    echo "  $0 -a amd64 -b alpine  # 构建 Alpine AMD64 版本"
    echo "  $0 -p                  # 构建并推送所有镜像"
    echo ""
}

# 解析命令行参数
ARCH="all"
BASE_IMAGE=""
PUSH=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -a|--arch)
            ARCH="$2"
            shift 2
            ;;
        -b|--base)
            BASE_IMAGE="$2"
            shift 2
            ;;
        -p|--push)
            PUSH=true
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            show_help
            exit 1
            ;;
    esac
done

# 验证架构参数
if [[ "$ARCH" != "all" && "$ARCH" != "amd64" && "$ARCH" != "arm64" ]]; then
    echo -e "${RED}错误: 不支持的架构 '$ARCH'${NC}"
    exit 1
fi

# 验证基础镜像参数
if [[ -n "$BASE_IMAGE" ]]; then
    valid_base=false
    for img in "${BASE_IMAGES[@]}"; do
        if [[ "$img" == "$BASE_IMAGE" ]]; then
            valid_base=true
            break
        fi
    done
    if [[ "$valid_base" == "false" ]]; then
        echo -e "${RED}错误: 不支持的基础镜像 '$BASE_IMAGE'${NC}"
        echo "支持的基础镜像: ${BASE_IMAGES[*]}"
        exit 1
    fi
fi

# 设置要构建的架构列表
if [[ "$ARCH" == "all" ]]; then
    BUILD_ARCHS=("${ARCHITECTURES[@]}")
else
    BUILD_ARCHS=("$ARCH")
fi

# 设置要构建的基础镜像列表
if [[ -n "$BASE_IMAGE" ]]; then
    BUILD_BASES=("$BASE_IMAGE")
else
    BUILD_BASES=("${BASE_IMAGES[@]}")
fi

# 创建多架构构建器
setup_builder() {
    echo -e "${BLUE}设置多架构构建器...${NC}"
    
    # 检查是否已存在构建器
    if ! docker buildx inspect multiarch-builder >/dev/null 2>&1; then
        echo "创建新的多架构构建器..."
        docker buildx create --name multiarch-builder --use --bootstrap
    else
        echo "使用现有的多架构构建器..."
        docker buildx use multiarch-builder
    fi
    
    echo -e "${GREEN}多架构构建器设置完成${NC}"
}

# 构建单个镜像
build_image() {
    local base_image="$1"
    local arch="$2"
    
    # 从基础镜像名称提取发行版名称
    local distro=$(echo "$base_image" | cut -d: -f1)
    local tag=$(echo "$base_image" | cut -d: -f2)
    
    # 构建镜像标签
    local image_tag="${REGISTRY}/${PROJECT_NAME}:${VERSION}-${distro}-${arch}"
    local latest_tag="${REGISTRY}/${PROJECT_NAME}:latest-${distro}-${arch}"
    
    echo -e "${BLUE}构建镜像: ${image_tag}${NC}"
    echo "基础镜像: $base_image"
    echo "架构: $arch"
    
    # 构建镜像
    docker buildx build \
        --platform "linux/$arch" \
        --build-arg "BASE_IMAGE=$base_image" \
        --tag "$image_tag" \
        --tag "$latest_tag" \
        --file Dockerfile \
        --load \
        .
    
    if [[ $? -eq 0 ]]; then
        echo -e "${GREEN}✅ 镜像构建成功: ${image_tag}${NC}"
        
        # 如果需要推送
        if [[ "$PUSH" == "true" ]]; then
            echo -e "${YELLOW}推送镜像到仓库...${NC}"
            docker push "$image_tag"
            docker push "$latest_tag"
            echo -e "${GREEN}✅ 镜像推送成功${NC}"
        fi
    else
        echo -e "${RED}❌ 镜像构建失败: ${image_tag}${NC}"
        return 1
    fi
}

# 主构建流程
main() {
    echo -e "${BLUE}开始构建 HeadCNI 多架构镜像${NC}"
    echo "版本: $VERSION"
    echo "架构: ${BUILD_ARCHS[*]}"
    echo "基础镜像: ${BUILD_BASES[*]}"
    echo "推送: $PUSH"
    echo ""
    
    # 设置构建器
    setup_builder
    
    # 构建所有组合
    local success_count=0
    local total_count=0
    
    for base_image in "${BUILD_BASES[@]}"; do
        for arch in "${BUILD_ARCHS[@]}"; do
            total_count=$((total_count + 1))
            echo ""
            echo "=========================================="
            echo "构建进度: $total_count / $(( ${#BUILD_BASES[@]} * ${#BUILD_ARCHS[@]} ))"
            echo "=========================================="
            
            if build_image "$base_image" "$arch"; then
                success_count=$((success_count + 1))
            fi
        done
    done
    
    echo ""
    echo "=========================================="
    echo "构建完成"
    echo "=========================================="
    echo "成功: $success_count / $total_count"
    
    if [[ $success_count -eq $total_count ]]; then
        echo -e "${GREEN}🎉 所有镜像构建成功!${NC}"
        exit 0
    else
        echo -e "${RED}❌ 部分镜像构建失败${NC}"
        exit 1
    fi
}

# 运行主函数
main "$@" 