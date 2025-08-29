#!/bin/bash

# HeadCNI Plugin 多平台构建脚本（修复版）
# 解决镜像标记和 manifest 创建问题

set -e

# 配置
REGISTRY="${REGISTRY:-docker.io}"
NAMESPACE="${NAMESPACE:-binrc}"
IMAGE_NAME="${IMAGE_NAME:-headcni}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# 支持的平台 (按构建速度排序)
PLATFORMS=(
    "linux/amd64"      # 最快 (x86_64)
    "linux/arm64"      # 较快 (ARM64)
    # "linux/386"        # 较快 (x86)
    # "linux/arm/v7"     # 中等 (ARMv7)
    # "linux/arm/v8"     # 中等 (ARMv8)
    # "linux/ppc64le"    # 较慢 (PowerPC)
    # "linux/s390x"      # 较慢 (IBM S390x)
    # "linux/riscv64"    # 最慢 (RISC-V)
    # "darwin/amd64"     # 较快 (macOS Intel)
    # "darwin/arm64"     # 较快 (macOS Apple Silicon)
    # "windows/amd64"    # 中等 (Windows x64)
    # "windows/arm64"    # 中等 (Windows ARM64)
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
    
    # 记录构建开始时间
    local start_time=$(date +%s)
    
    # 构建镜像到 buildx 缓存，不使用 --load 避免并发限制
    if docker buildx build \
        --platform "$platform" \
        --tag "$image_tag" \
        --file .docker/Dockerfile.local \
        --progress=plain \
        --build-arg TARGETOS=$(echo "$platform" | cut -d'/' -f1) \
        --build-arg TARGETARCH=$(echo "$platform" | cut -d'/' -f2) \
        --cache-from type=local,src=/tmp/.buildx-cache \
        --cache-to type=local,dest=/tmp/.buildx-cache,mode=max \
        . > "/tmp/headcni_build_${os_arch}.log" 2>&1; then
        
        # 计算构建时间
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        
        log_info "✓ 平台镜像构建完成: $image_tag (耗时: ${duration}s)"
        
        # 返回镜像标签
        echo "$image_tag"
        return 0
    else
        log_error "✗ 平台镜像构建失败: $platform"
        log_error "构建日志: /tmp/headcni_build_${os_arch}.log"
        return 1
    fi
}

# 并行构建多个平台
build_platforms_parallel() {
    # 动态计算最优并行数
    local cpu_cores=$(nproc 2>/dev/null || echo 4)
    local available_memory=$(free -m 2>/dev/null | awk '/^Mem:/{print $2}' || echo 8192)
    
    # 根据系统资源调整并行数
    local max_jobs=4  # 默认值
    if [ "$cpu_cores" -ge 32 ] && [ "$available_memory" -ge 32768 ]; then
        max_jobs=12  # 顶级高性能系统 (64核+)
    elif [ "$cpu_cores" -ge 16 ] && [ "$available_memory" -ge 16384 ]; then
        max_jobs=8   # 高性能系统
    elif [ "$cpu_cores" -ge 8 ] && [ "$available_memory" -ge 8192 ]; then
        max_jobs=6   # 中等高性能系统
    elif [ "$cpu_cores" -ge 4 ] && [ "$available_memory" -ge 4096 ]; then
        max_jobs=4   # 中等性能系统
    else
        max_jobs=2   # 低性能系统
    fi
    
    log_info "系统资源: CPU核心=$cpu_cores, 内存=${available_memory}MB"
    log_info "设置并行构建数: $max_jobs"
    
    local built_images=()
    
    log_step "开始构建平台镜像..."
    
    # 创建临时目录存储结果
    local temp_dir=$(mktemp -d)
    local result_file="$temp_dir/build_results.txt"
    
    # 使用后台作业和作业控制进行并行构建
    local job_count=0
    local platform_index=0
    
    while [ $platform_index -lt ${#PLATFORMS[@]} ]; do
        # 检查当前运行的作业数
        while [ $(jobs -r | wc -l) -ge $max_jobs ]; do
            sleep 1
        done
        
        # 启动新的构建作业
        local platform="${PLATFORMS[$platform_index]}"
        log_info "启动后台构建: $platform (作业 $((job_count + 1)))"
        
        (
            if image_tag=$(build_platform "$platform"); then
                echo "$image_tag" >> "$result_file"
                log_info "✓ 后台构建完成: $platform -> $image_tag"
            else
                log_error "✗ 后台构建失败: $platform"
                exit 1
            fi
        ) &
        
        job_count=$((job_count + 1))
        platform_index=$((platform_index + 1))
        
        # 短暂延迟，避免同时启动过多作业
        sleep 0.5
    done
    
    # 等待所有后台作业完成，显示进度
    log_info "等待所有构建作业完成..."
    
    # 显示进度
    local total=${#PLATFORMS[@]}
    
    # 等待所有后台作业完成，显示进度
    log_info "等待所有构建作业完成..."
    
    # 使用文件计数来跟踪进度
    local result_file="$temp_dir/build_results.txt"
    local completed=0
    local total=${#PLATFORMS[@]}
    
    while [ $completed -lt $total ]; do
        # 计算已完成的作业数（通过结果文件）
        if [ -f "$result_file" ]; then
            completed=$(wc -l < "$result_file" 2>/dev/null || echo 0)
        else
            completed=0
        fi
        
        # 计算正在运行的作业数
        local running=$(jobs -r | wc -l)
        
        # 显示进度
        log_info "构建进度: $completed/$total 完成, $running 运行中..."
        
        # 如果所有作业都完成了，退出循环
        if [ $completed -eq $total ]; then
            break
        fi
        
        # 等待一段时间再检查
        sleep 10
    done
    
    # 最终状态检查
    local final_running=$(jobs -r | wc -l)
    if [ $final_running -eq 0 ]; then
        log_info "✓ 所有构建作业已完成"
    else
        log_warn "⚠ 仍有 $final_running 个作业在运行，等待完成..."
        wait
    fi
    
    wait
    
    # 检查是否有作业失败
    if [ $? -ne 0 ]; then
        log_error "部分构建作业失败"
        return 1
    fi
    
    log_info "✓ 所有构建作业完成"
    
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

# 从 buildx 缓存导出镜像到本地
export_images_from_cache() {
    log_step "从 buildx 缓存导出镜像到本地..."
    
    if [ ! -f .docker/.built_platforms.txt ]; then
        log_error "找不到构建结果文件"
        return 1
    fi
    
    while IFS= read -r image; do
        if [ -n "$image" ]; then
            log_info "导出镜像到本地: $image"
            
            # 使用 docker buildx imagetools 检查镜像是否存在
            if docker buildx imagetools inspect "$image" > /dev/null 2>&1; then
                # 创建临时 Dockerfile 来导出镜像
                local temp_dockerfile="/tmp/export_${RANDOM}.Dockerfile"
                echo "FROM $image" > "$temp_dockerfile"
                
                # 导出镜像到本地
                docker buildx build --load -f "$temp_dockerfile" -t "$image" . > /dev/null 2>&1
                
                # 清理临时文件
                rm -f "$temp_dockerfile"
                
                log_info "✓ 镜像导出完成: $image"
            else
                log_error "✗ 镜像不存在于 buildx 缓存: $image"
            fi
        fi
    done < .docker/.built_platforms.txt
    
    log_info "所有镜像导出完成"
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
            export_images_from_cache
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
            export_images_from_cache
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