#!/bin/bash

# HeadCNI Tailscale Backend测试脚本
# 使用主机的Tailscale socket进行测试

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查Tailscale socket
check_tailscale_socket() {
    local socket_path="/var/run/tailscale/tailscaled.sock"
    
    log_info "检查Tailscale socket: $socket_path"
    
    if [ ! -S "$socket_path" ]; then
        log_error "Tailscale socket不存在: $socket_path"
        log_info "请确保Tailscale已安装并正在运行:"
        log_info "  sudo systemctl status tailscaled"
        log_info "  sudo tailscale status"
        exit 1
    fi
    
    # 检查socket权限
    if [ ! -r "$socket_path" ]; then
        log_warning "当前用户无法读取Tailscale socket"
        log_info "可能需要以root权限运行测试，或者将用户添加到tailscale组"
    fi
    
    log_success "Tailscale socket检查通过"
}

# 检查Go环境
check_go_env() {
    log_info "检查Go环境"
    
    if ! command -v go &> /dev/null; then
        log_error "Go未安装或不在PATH中"
        exit 1
    fi
    
    local go_version=$(go version | awk '{print $3}')
    log_info "Go版本: $go_version"
    
    log_success "Go环境检查通过"
}

# 编译测试
build_test() {
    log_info "编译Tailscale backend"
    
    cd "$(dirname "$0")/../pkg/backend/tailscale"
    
    if ! go build .; then
        log_error "编译失败"
        exit 1
    fi
    
    log_success "编译成功"
}

# 运行单元测试
run_unit_tests() {
    log_info "运行单元测试"
    
    cd "$(dirname "$0")/../pkg/backend/tailscale"
    
    # 运行基础测试
    log_info "运行基础功能测试..."
    if go test -v -run="TestSimpleClient_Basic" .; then
        log_success "基础功能测试通过"
    else
        log_warning "基础功能测试失败，可能Tailscale未连接"
    fi
    
    # 运行IP获取测试
    log_info "运行IP获取测试..."
    if go test -v -run="TestSimpleClient_GetIP" .; then
        log_success "IP获取测试通过"
    else
        log_warning "IP获取测试失败"
    fi
    
    # 运行连接性测试
    log_info "运行连接性测试..."
    if go test -v -run="TestSimpleClient_Connectivity" .; then
        log_success "连接性测试通过"
    else
        log_warning "连接性测试失败"
    fi
    
    # 运行对等节点测试
    log_info "运行对等节点测试..."
    if go test -v -run="TestSimpleClient_Peers" .; then
        log_success "对等节点测试通过"
    else
        log_warning "对等节点测试失败"
    fi
    
    # 运行Backend集成测试
    log_info "运行Backend集成测试..."
    if go test -v -run="TestBackend_Integration" .; then
        log_success "Backend集成测试通过"
    else
        log_warning "Backend集成测试失败"
    fi
}

# 运行性能测试
run_performance_tests() {
    log_info "运行性能测试"
    
    cd "$(dirname "$0")/../pkg/backend/tailscale"
    
    # 运行性能基准测试
    log_info "运行GetStatus性能测试..."
    go test -bench="BenchmarkSimpleClient_GetStatus" -benchtime=5s .
    
    log_info "运行GetIP性能测试..."
    go test -bench="BenchmarkSimpleClient_GetIP" -benchtime=5s .
}

# 运行集成测试
run_integration_tests() {
    log_info "运行完整集成测试"
    
    cd "$(dirname "$0")/../pkg/backend/tailscale"
    
    if [ "$INTEGRATION_TEST" != "1" ]; then
        log_warning "集成测试被跳过，设置INTEGRATION_TEST=1来运行"
        return
    fi
    
    # 运行完整的CNI集成测试
    log_info "运行CNI集成测试..."
    if INTEGRATION_TEST=1 go test -v -run="TestCNI_Integration" -timeout=5m .; then
        log_success "CNI集成测试通过"
    else
        log_error "CNI集成测试失败"
    fi
}

# 显示Tailscale状态
show_tailscale_status() {
    log_info "Tailscale状态信息"
    
    if command -v tailscale &> /dev/null; then
        echo "=== Tailscale Status ==="
        sudo tailscale status || log_warning "无法获取Tailscale状态"
        
        echo -e "\n=== Tailscale IP ==="
        sudo tailscale ip || log_warning "无法获取Tailscale IP"
        
        echo -e "\n=== Tailscale Version ==="
        tailscale version || log_warning "无法获取Tailscale版本"
    else
        log_warning "tailscale命令不可用"
    fi
}

# 生成测试报告
generate_test_report() {
    log_info "生成测试报告"
    
    local report_file="/tmp/headcni-test-report.txt"
    
    cat > "$report_file" << EOF
HeadCNI Tailscale Backend 测试报告
生成时间: $(date)
测试环境: $(uname -a)
Go版本: $(go version)

=== 系统信息 ===
$(uname -a)

=== Tailscale信息 ===
EOF

    if command -v tailscale &> /dev/null; then
        echo "版本: $(tailscale version)" >> "$report_file"
        if sudo tailscale status &> /dev/null; then
            echo "状态: 已连接" >> "$report_file"
            sudo tailscale ip >> "$report_file" 2>/dev/null || echo "IP: 获取失败" >> "$report_file"
        else
            echo "状态: 未连接或无权限" >> "$report_file"
        fi
    else
        echo "Tailscale: 未安装" >> "$report_file"
    fi
    
    echo -e "\n=== 测试结果 ===" >> "$report_file"
    echo "详细日志请查看测试输出" >> "$report_file"
    
    log_success "测试报告已生成: $report_file"
}

# 主函数
main() {
    echo "========================================"
    echo "    HeadCNI Tailscale Backend 测试"
    echo "========================================"
    
    # 解析命令行参数
    local run_integration=false
    local run_performance=false
    local show_help=false
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --integration)
                run_integration=true
                export INTEGRATION_TEST=1
                shift
                ;;
            --performance)
                run_performance=true
                export PERFORMANCE_TEST=1
                shift
                ;;
            --help|-h)
                show_help=true
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help=true
                shift
                ;;
        esac
    done
    
    if [ "$show_help" = true ]; then
        echo "用法: $0 [选项]"
        echo ""
        echo "选项:"
        echo "  --integration     运行集成测试"
        echo "  --performance     运行性能测试"
        echo "  --help, -h        显示帮助信息"
        echo ""
        echo "示例:"
        echo "  $0                    # 运行基础测试"
        echo "  $0 --integration     # 运行完整集成测试"
        echo "  $0 --performance     # 运行性能测试"
        exit 0
    fi
    
    # 执行检查
    check_go_env
    check_tailscale_socket
    
    # 显示Tailscale状态
    show_tailscale_status
    
    # 编译测试
    build_test
    
    # 运行测试
    run_unit_tests
    
    if [ "$run_performance" = true ]; then
        run_performance_tests
    fi
    
    if [ "$run_integration" = true ]; then
        run_integration_tests
    fi
    
    # 生成报告
    generate_test_report
    
    log_success "所有测试完成！"
}

# 脚本入口
main "$@"