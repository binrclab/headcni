#!/bin/bash

# HeadCNI Plugin 多平台构建脚本（修复版）
# 解决镜像标记和 manifest 创建问题

set -e

# 配置
REGISTRY="${REGISTRY:-docker.io}"
NAMESPACE="${NAMESPACE:-binrc}"
IMAGE_NAME="${IMAGE_NAME:-headcni}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# 支持的平台
PLATFORMS=(
    "linux/amd64"      # 最快
    "linux/arm64"      # 较快
    "linux/arm/v7"     # 中等
    "linux/arm/v8"     # 中等
    "linux/386"        # 较快
    "linux/ppc64le"    # 较慢
    "linux/s390x"      # 较慢
    "linux/riscv64"    # 最慢
    # "darwin/amd64"     # 较快
    # "darwin/arm64"     # 较快
    # "windows/amd64"    # 中等
    # "windows/arm64"    # 中等
)

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1" >&2
}

# 检查依赖
check_dependencies() {
    log_step "检查依赖..."
    
    if ! command -v docker &> /dev/null; then
        log_error "Docker 未安装"
        exit 1
    fi
    
    if ! command -v docker buildx &> /dev/null; then
        log_error "Docker Buildx 未安装"
        exit 1
    fi
    
    log_info "依赖检查通过"
}

# 设置多架构构建器
setup_local_builder() {
    log_step "设置多架构构建器..."

    # 确保 QEMU 已安装，支持跨架构构建
    if ! docker run --privileged --rm tonistiigi/binfmt --install all >/dev/null 2>&1; then
        log_error "安装 QEMU (binfmt) 失败"
        exit 1
    fi
    log_info "QEMU 已安装，支持跨架构构建"

    # 检查是否已有 multi-builder
    if docker buildx ls | grep -q "multi-builder"; then
        log_info "使用已有的 multi-builder"
        docker buildx use multi-builder
    else
        log_info "创建新的 multi-builder"
        docker buildx create --name multi-builder --driver docker-container --use
    fi

    # 检查构建器状态
    docker buildx inspect --bootstrap
}

# 构建单个平台镜像
build_platform() {
    local platform=$1
    local os_arch=$(echo "$platform" | sed 's/\//-/g')
    local image_tag="${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}-${os_arch}"
    
    log_info "构建平台: $platform -> $image_tag"
    
    # 先构建到本地，不使用 --push 参数
    docker buildx build \
        --platform "$platform" \
        --tag "$image_tag" \
        --file .docker/Dockerfile.local \
        --progress=plain \
        --build-arg TARGETOS=$(echo "$platform" | cut -d'/' -f1) \
        --build-arg TARGETARCH=$(echo "$platform" | cut -d'/' -f2) \
        --load .
    
    log_info "平台镜像构建完成: $image_tag"
    
    # 返回镜像标签
    echo "$image_tag"
}

# 并行构建多个平台
build_platforms_parallel() {
    local max_jobs=1  # 降低并行数，避免 --load 冲突
    local built_images=()
    
    log_step "开始构建平台镜像..."
    
    # 创建临时目录存储结果
    local temp_dir=$(mktemp -d)
    local result_file="$temp_dir/build_results.txt"
    
    # 逐个构建平台镜像（避免 --load 冲突）
    for platform in "${PLATFORMS[@]}"; do
        log_info "开始构建平台: $platform"
        
        if image_tag=$(build_platform "$platform"); then
            echo "$image_tag" >> "$result_file"
            log_info "成功构建: $image_tag"
        else
            log_error "构建失败: $platform"
            return 1
        fi
        
        # 添加短暂延迟
        sleep 2
    done
    
    # 读取构建结果
    if [ -f "$result_file" ]; then
        while IFS= read -r image; do
            if [ -n "$image" ]; then
                built_images+=("$image")
            fi
        done < "$result_file"
    fi
    
    # 清理临时目录
    rm -rf "$temp_dir"
    
    log_info "所有平台镜像构建完成，共 ${#built_images[@]} 个镜像"
    
    # 保存镜像标签到文件
    if [ ${#built_images[@]} -gt 0 ]; then
        printf "%s\n" "${built_images[@]}" > .docker/.built_platforms.txt
        log_info "保存的镜像标签:"
        for img in "${built_images[@]}"; do
            log_info "  - $img"
        done
    else
        log_error "没有成功构建的镜像"
        return 1
    fi
    
    return 0
}

# 推送平台镜像到远程仓库
push_platform_images() {
    log_step "推送平台镜像到远程仓库..."
    
    if [ ! -f .docker/.built_platforms.txt ]; then
        log_error "找不到构建结果文件"
        return 1
    fi
    
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            log_info "推送镜像: $image"
            docker push "$image"
        fi
    done < .docker/.built_platforms.txt
    
    log_info "所有平台镜像推送完成"
}

# 创建多平台 manifest
create_manifest() {
    local skip_push="${SKIP_PUSH:-false}"  # 默认推送 manifest
    log_step "创建多平台 manifest..."
    
    local manifest_tag="${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    log_info "创建统一 manifest: $manifest_tag"
    
    # 收集所有平台镜像
    local platform_images=()
    if [ ! -f .docker/.built_platforms.txt ]; then
        log_error "找不到构建结果文件"
        return 1
    fi
    
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            platform_images+=("$image")
            log_info "添加平台镜像: $image"
        fi
    done < .docker/.built_platforms.txt
    
    # 检查是否有镜像
    if [ ${#platform_images[@]} -eq 0 ]; then
        log_error "没有找到平台镜像，无法创建 manifest"
        return 1
    fi
    
    log_info "准备创建 manifest，包含 ${#platform_images[@]} 个平台镜像"
    
    # 验证本地镜像存在
    for img in "${platform_images[@]}"; do
        if ! docker image inspect "$img" > /dev/null 2>&1; then
            log_error "本地镜像不存在: $img"
            return 1
        fi
        log_info "验证本地镜像存在: $img"
    done
    
    # 先推送所有平台镜像到远程仓库
    push_platform_images
    
    # 删除已存在的 manifest（如果有）
    docker manifest rm "$manifest_tag" 2>/dev/null || true
    
    # 创建新的 manifest
    log_info "创建 manifest: $manifest_tag"
    docker manifest create "$manifest_tag" "${platform_images[@]}"
    
    # 根据设置决定是否推送manifest
    if [ "$skip_push" = "true" ]; then
        log_info "跳过manifest推送"
        log_info "如需推送manifest，请设置 SKIP_PUSH=false"
    else
        log_info "推送 manifest 到远程仓库..."
        docker manifest push "$manifest_tag"
    fi
    
    log_info "✓ 多平台 manifest 创建完成: $manifest_tag"
    
    # 保存 manifest 标签到文件
    echo "$manifest_tag" > .docker/.manifest_tag.txt
    
    return 0
}

# 验证镜像
verify_images() {
    log_step "验证构建的镜像..."
    
    local manifest_tag="${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    # 检查本地平台镜像
    log_info "检查本地平台镜像..."
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            if docker image inspect "$image" > /dev/null 2>&1; then
                log_info "✓ 本地镜像存在: $image"
            else
                log_error "✗ 本地镜像不存在: $image"
            fi
        fi
    done < .docker/.built_platforms.txt
    
    # 检查 manifest
    log_info "检查 manifest..."
    if docker manifest inspect "$manifest_tag" > /dev/null 2>&1; then
        log_info "✓ Manifest 验证通过: $manifest_tag"
        
        # 显示支持的平台
        docker manifest inspect "$manifest_tag" | jq -r '.manifests[] | "\(.platform.os)/\(.platform.architecture)"' 2>/dev/null | while read platform; do
            log_info "  支持平台: $platform"
        done
    else
        log_warn "✗ Manifest 验证失败或不存在: $manifest_tag"
    fi
    
    return 0
}

# 清理临时文件
cleanup() {
    log_step "清理临时文件..."
    
    rm -f .docker/.built_platforms.txt
    rm -f .docker/.manifest_tag.txt
    rm -f /tmp/headcni_build_*.txt 2>/dev/null || true
    
    log_info "✓ 清理完成"
}

# 主函数
main() {
    local action="${1:-all}"
    
    case "$action" in
        "all")
            log_step "开始完整的多架构构建流程..."
            check_dependencies
            setup_local_builder
            build_platforms_parallel
            create_manifest
            verify_images
            cleanup
            log_info "✓ 多架构构建流程完成"
            ;;
        "build")
            log_step "开始构建平台镜像..."
            check_dependencies
            setup_local_builder
            build_platforms_parallel
            log_info "✓ 平台镜像构建完成"
            ;;
        "push")
            log_step "推送平台镜像..."
            push_platform_images
            log_info "✓ 平台镜像推送完成"
            ;;
        "manifest")
            log_step "创建多平台 manifest..."
            create_manifest
            log_info "✓ Manifest 创建完成"
            ;;
        "verify")
            log_step "验证镜像..."
            verify_images
            log_info "✓ 镜像验证完成"
            ;;
        "cleanup")
            log_step "清理..."
            cleanup
            log_info "✓ 清理完成"
            ;;
        *)
            log_error "未知操作: $action"
            echo "用法: $0 [all|build|push|manifest|verify|cleanup]"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@"