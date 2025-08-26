#!/bin/bash

# HeadCNI Plugin 多平台构建脚本（优化版）
# 使用本地构建器，避免拉取远程镜像

set -e

# 配置
REGISTRY="${REGISTRY:-docker.io}"
NAMESPACE="${NAMESPACE:-binrc}"
IMAGE_NAME="${IMAGE_NAME:-headcni-plugin}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# 支持的平台（按构建速度排序）
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
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
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

# 设置本地构建器（快速模式）
setup_local_builder() {
    log_step "设置本地构建器（快速模式）..."
    
    # 使用默认的本地构建器
    if docker buildx ls | grep -q "default"; then
        log_info "使用默认本地构建器"
        docker buildx use default
    else
        log_info "创建本地构建器"
        docker buildx create --name local-builder --driver docker --use
    fi
    
    # 检查构建器状态
    docker buildx inspect
}

# 构建单个平台镜像
build_platform() {
    local platform=$1
    local os_arch=$(echo "$platform" | sed 's/\//-/g')
    local image_tag="${REGISTRY}/${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}-${os_arch}"
    
    log_step "构建平台: $platform -> $image_tag"
    
    # 使用本地构建器构建镜像
    docker buildx build \
        --platform "$platform" \
        --tag "$image_tag" \
        --load \
        --file .docker/Dockerfile \
        --progress=plain \
        .
    
    log_info "✓ 平台镜像构建完成: $image_tag"
    
    # 返回镜像标签
    echo "$image_tag"
}

# 并行构建多个平台
build_platforms_parallel() {
    local max_jobs=4  # 最大并行数
    local pids=()
    local built_images=()
    local job_count=0
    
    log_step "开始并行构建平台镜像（最大并行数: $max_jobs）..."
    
    for platform in "${PLATFORMS[@]}"; do
        # 等待有空闲的构建槽
        while [ ${#pids[@]} -ge $max_jobs ]; do
            for i in "${!pids[@]}"; do
                if ! kill -0 "${pids[$i]}" 2>/dev/null; then
                    # 进程已完成，移除
                    unset "pids[$i]"
                    pids=("${pids[@]}")  # 重新索引
                fi
            done
            sleep 1
        done
        
        # 启动新的构建任务
        (
            image_tag=$(build_platform "$platform")
            echo "$image_tag" > "/tmp/headcni_build_${RANDOM}.txt"
        ) &
        
        local pid=$!
        pids+=("$pid")
        job_count=$((job_count + 1))
        
        log_info "启动构建任务 $job_count: $platform (PID: $pid)"
    done
    
    # 等待所有构建任务完成
    log_step "等待所有构建任务完成..."
    for pid in "${pids[@]}"; do
        wait "$pid"
    done
    
    # 收集构建结果
    log_step "收集构建结果..."
    for tmpfile in /tmp/headcni_build_*.txt; do
        if [ -f "$tmpfile" ]; then
            local image_tag=$(cat "$tmpfile")
            built_images+=("$image_tag")
            rm "$tmpfile"
        fi
    done
    
    log_info "所有平台镜像构建完成，共 ${#built_images[@]} 个镜像"
    
    # 保存镜像标签到文件
    printf "%s\n" "${built_images[@]}" > .docker/.built_platforms.txt
    
    return 0
}

# 创建多平台 manifest
create_manifest() {
    log_step "创建多平台 manifest..."
    
    local manifest_tag="${REGISTRY}/${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    log_info "创建统一 manifest: $manifest_tag"
    
    # 收集所有平台镜像
    local platform_images=()
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            platform_images+=("$image")
        fi
    done < .docker/.built_platforms.txt
    
    # 创建 manifest
    docker manifest create "$manifest_tag" "${platform_images[@]}"
    
    # 推送 manifest
    docker manifest push "$manifest_tag"
    
    log_info "✓ 多平台 manifest 创建完成: $manifest_tag"
    
    # 保存 manifest 标签到文件
    echo "$manifest_tag" > .docker/.manifest_tag.txt
    
    return 0
}

# 验证镜像
verify_images() {
    log_step "验证多平台镜像..."
    
    if [ -f .docker/.manifest_tag.txt ]; then
        local manifest_tag=$(cat .docker/.manifest_tag.txt)
        log_info "验证 manifest: $manifest_tag"
        
        # 检查 manifest
        docker buildx imagetools inspect "$manifest_tag"
        
        # 测试几个主要平台
        local test_platforms=("linux/amd64" "linux/arm64")
        for platform in "${test_platforms[@]}"; do
            log_info "测试平台: $platform"
            docker run --rm --platform="$platform" "$manifest_tag" || log_warn "平台 $platform 测试失败"
        done
    fi
    
    log_info "镜像验证完成"
}

# 推送镜像
push_images() {
    log_step "推送镜像..."
    
    if [ -z "$PUSH_IMAGES" ] || [ "$PUSH_IMAGES" != "true" ]; then
        log_info "跳过镜像推送 (设置 PUSH_IMAGES=true 启用推送)"
        return 0
    fi
    
    # 推送所有平台镜像
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            log_info "推送镜像: $image"
            docker push "$image"
        fi
    done < .docker/.built_platforms.txt
    
    # 推送 manifest
    if [ -f .docker/.manifest_tag.txt ]; then
        local manifest_tag=$(cat .docker/.manifest_tag.txt)
        log_info "推送 manifest: $manifest_tag"
        docker push "$manifest_tag"
    fi
    
    log_info "所有镜像推送完成"
}

# 清理资源
cleanup() {
    log_step "清理构建资源..."
    
    rm -f .docker/.built_platforms.txt .docker/.manifest_tag.txt
    
    if [ "${CLEANUP_IMAGES:-false}" = "true" ]; then
        log_info "清理本地镜像..."
        
        while IFS= read -r image; do
            if [ -n "$image" ]; then
                docker rmi "$image" 2>/dev/null || true
            fi
        done < .docker/.built_platforms.txt
    fi
    
    log_info "清理完成"
}

# 显示帮助信息
show_help() {
    cat << EOF
HeadCNI Plugin 多平台构建脚本（优化版）

用法: $0 [选项] [命令]

命令:
    build          构建所有平台的镜像（并行）
    manifest       创建多平台 manifest
    verify         验证已构建的镜像
    push           推送镜像到注册表
    cleanup        清理构建资源
    all            执行完整流程 (build + manifest + verify + push)

选项:
    -h, --help     显示此帮助信息
    -r, --registry 指定 Docker 注册表 (默认: docker.io)
    -n, --namespace 指定命名空间 (默认: binrc)
    -i, --image    指定镜像名称 (默认: headcni-plugin)
    -t, --tag      指定镜像标签 (默认: latest)
    -c, --cleanup  构建完成后清理本地镜像
    -j, --jobs     指定最大并行构建数 (默认: 4)

环境变量:
    REGISTRY        Docker 注册表
    NAMESPACE       命名空间
    IMAGE_NAME      镜像名称
    IMAGE_TAG       镜像标签
    PUSH_IMAGES     是否推送镜像 (true/false)
    CLEANUP_IMAGES  是否清理本地镜像 (true/false)

性能优化:
    - 使用本地构建器，避免拉取远程镜像
    - 并行构建多个平台，提高构建速度
    - 按构建速度排序平台，优先构建快速平台

示例:
    $0 build                    # 并行构建所有平台镜像
    $0 all                      # 执行完整流程
    $0 -j 6 build              # 使用6个并行任务构建
    PUSH_IMAGES=true $0 all    # 执行完整流程并推送镜像

EOF
}

# 主函数
main() {
    local command=""
    local cleanup_images=false
    local max_jobs=4
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -r|--registry)
                REGISTRY="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -i|--image)
                IMAGE_NAME="$2"
                shift 2
                ;;
            -t|--tag)
                IMAGE_TAG="$2"
                shift 2
                ;;
            -c|--cleanup)
                cleanup_images=true
                shift
                ;;
            -j|--jobs)
                max_jobs="$2"
                shift 2
                ;;
            build|manifest|verify|push|cleanup|all)
                command="$1"
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 设置清理选项
    if [ "$cleanup_images" = "true" ]; then
        export CLEANUP_IMAGES=true
    fi
    
    # 如果没有指定命令，默认执行完整流程
    if [ -z "$command" ]; then
        command="all"
    fi
    
    log_step "开始 HeadCNI Plugin 多平台构建流程（优化版）"
    log_info "注册表: $REGISTRY"
    log_info "命名空间: $NAMESPACE"
    log_info "镜像名称: $IMAGE_NAME"
    log_info "镜像标签: $IMAGE_TAG"
    log_info "支持平台: ${PLATFORMS[*]}"
    log_info "最大并行数: $max_jobs"
    
    case "$command" in
        "build")
            check_dependencies
            setup_local_builder
            build_platforms_parallel
            ;;
        "manifest")
            check_dependencies
            setup_local_builder
            create_manifest
            ;;
        "verify")
            verify_images
            ;;
        "push")
            push_images
            ;;
        "cleanup")
            cleanup
            ;;
        "all")
            check_dependencies
            setup_local_builder
            build_platforms_parallel
            create_manifest
            verify_images
            push_images
            ;;
        *)
            log_error "未知命令: $command"
            show_help
            exit 1
            ;;
    esac
    
    log_info "构建流程完成"
}

# 运行主函数
main "$@" 