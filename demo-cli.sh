#!/bin/bash

# HeadCNI CLI 演示脚本
set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== HeadCNI CLI 工具演示 ===${NC}"
echo ""

# 检查CLI工具是否存在
if [ ! -f "./bin/headcni-cli" ]; then
    echo -e "${RED}错误: CLI工具未构建，请先运行 'make build-cli'${NC}"
    exit 1
fi

echo -e "${GREEN}✅ CLI工具已构建${NC}"
echo ""

# 显示版本信息
echo -e "${YELLOW}📋 版本信息:${NC}"
./bin/headcni-cli --version
echo ""

# 显示帮助信息
echo -e "${YELLOW}📋 帮助信息:${NC}"
./bin/headcni-cli --help
echo ""

# 显示所有可用命令
echo -e "${YELLOW}📋 可用命令:${NC}"
./bin/headcni-cli --help | grep -A 10 "Available Commands"
echo ""

# 演示各个命令的帮助信息
echo -e "${YELLOW}📋 安装命令帮助:${NC}"
./bin/headcni-cli install --help | head -20
echo ""

echo -e "${YELLOW}📋 状态检查命令帮助:${NC}"
./bin/headcni-cli status --help | head -20
echo ""

echo -e "${YELLOW}📋 连接测试命令帮助:${NC}"
./bin/headcni-cli connect-test --help | head -20
echo ""

echo -e "${YELLOW}📋 配置管理命令帮助:${NC}"
./bin/headcni-cli config --help | head -20
echo ""

echo -e "${YELLOW}📋 卸载命令帮助:${NC}"
./bin/headcni-cli uninstall --help | head -20
echo ""

# 演示logo显示（通过status命令）
echo -e "${YELLOW}📋 ASCII Logo 演示:${NC}"
echo "运行 'headcni status' 命令会显示以下logo:"
echo ""

# 创建一个临时脚本来显示logo
cat > /tmp/show_logo.go << 'EOF'
package main

import "fmt"

func main() {
	logo := `
██╗  ██╗███████╗ █████╗ ██████╗  ██████╗███╗   ██╗██╗
██║  ██║██╔════╝██╔══██╗██╔══██╗██╔════╝████╗  ██║██║
███████║█████╗  ███████║██║  ██║██║     ██╔██╗ ██║██║
██╔══██║██╔══╝  ██╔══██║██║  ██║██║     ██║╚██╗██║██║
██║  ██║███████╗██║  ██║██████╔╝╚██████╗██║ ╚████║██║
╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝  ╚═════╝╚═╝  ╚═══╝╚═╝
                                                      
    Kubernetes CNI Plugin for Headscale/Tailscale    
    ================================================
`
	fmt.Print(logo)
}
EOF

go run /tmp/show_logo.go
rm -f /tmp/show_logo.go

echo ""
echo -e "${GREEN}=== 演示完成 ===${NC}"
echo ""
echo -e "${BLUE}使用示例:${NC}"
echo "1. 安装 HeadCNI:"
echo "   ./bin/headcni-cli install --headscale-url https://headscale.company.com --auth-key YOUR_KEY"
echo ""
echo "2. 检查状态:"
echo "   ./bin/headcni-cli status"
echo ""
echo "3. 测试连接:"
echo "   ./bin/headcni-cli connect-test"
echo ""
echo "4. 查看配置:"
echo "   ./bin/headcni-cli config --show"
echo ""
echo "5. 卸载:"
echo "   ./bin/headcni-cli uninstall"
echo ""
echo -e "${YELLOW}注意: 请确保已连接到 Kubernetes 集群才能使用完整功能${NC}" 