#!/bin/bash

# HeadCNI Helm 部署脚本
set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 默认值
NAMESPACE="kube-system"
RELEASE_NAME="headcni"
HEADSCALE_URL=""
AUTH_KEY=""
POD_CIDR="10.244.0.0/16"
SERVICE_CIDR="10.96.0.0/16"
IPAM_TYPE="host-local"  # 或 "headcni-ipam"

# 帮助信息
show_help() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -n, --namespace     Kubernetes namespace (default: kube-system)"
    echo "  -r, --release       Helm release name (default: headcni)"
    echo "  -u, --headscale-url Headscale server URL (required)"
    echo "  -k, --auth-key      Headscale auth key (required)"
    echo "  -p, --pod-cidr      Pod CIDR (default: 10.244.0.0/16)"
    echo "  -s, --service-cidr  Service CIDR (default: 10.96.0.0/16)"
    echo "  -i, --ipam-type     IPAM type: host-local or headcni-ipam (default: host-local)"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Examples:"
    echo "  # 使用 host-local IPAM"
    echo "  $0 -u https://headscale.company.com -k YOUR_AUTH_KEY"
    echo ""
    echo "  # 使用 headcni-ipam"
    echo "  $0 -u https://headscale.company.com -k YOUR_AUTH_KEY -i headcni-ipam"
    echo ""
    echo "  # 自定义网络配置"
    echo "  $0 -u https://headscale.company.com -k YOUR_AUTH_KEY -p 10.42.0.0/16 -s 10.43.0.0/16"
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -r|--release)
            RELEASE_NAME="$2"
            shift 2
            ;;
        -u|--headscale-url)
            HEADSCALE_URL="$2"
            shift 2
            ;;
        -k|--auth-key)
            AUTH_KEY="$2"
            shift 2
            ;;
        -p|--pod-cidr)
            POD_CIDR="$2"
            shift 2
            ;;
        -s|--service-cidr)
            SERVICE_CIDR="$2"
            shift 2
            ;;
        -i|--ipam-type)
            IPAM_TYPE="$2"
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

# 验证必需参数
if [[ -z "$HEADSCALE_URL" ]]; then
    echo -e "${RED}Error: Headscale URL is required${NC}"
    show_help
    exit 1
fi

if [[ -z "$AUTH_KEY" ]]; then
    echo -e "${RED}Error: Headscale auth key is required${NC}"
    show_help
    exit 1
fi

# 验证IPAM类型
if [[ "$IPAM_TYPE" != "host-local" && "$IPAM_TYPE" != "headcni-ipam" ]]; then
    echo -e "${RED}Error: Invalid IPAM type. Must be 'host-local' or 'headcni-ipam'${NC}"
    exit 1
fi

echo -e "${GREEN}=== HeadCNI Helm 部署脚本 ===${NC}"
echo "Namespace: $NAMESPACE"
echo "Release Name: $RELEASE_NAME"
echo "Headscale URL: $HEADSCALE_URL"
echo "Pod CIDR: $POD_CIDR"
echo "Service CIDR: $SERVICE_CIDR"
echo "IPAM Type: $IPAM_TYPE"
echo ""

# 检查 Helm 是否安装
if ! command -v helm &> /dev/null; then
    echo -e "${RED}Error: Helm is not installed${NC}"
    exit 1
fi

# 检查 kubectl 是否安装
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}Error: kubectl is not installed${NC}"
    exit 1
fi

# 检查集群连接
echo -e "${YELLOW}检查 Kubernetes 集群连接...${NC}"
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}Error: Cannot connect to Kubernetes cluster${NC}"
    exit 1
fi

# 创建命名空间（如果不存在）
echo -e "${YELLOW}创建命名空间 $NAMESPACE...${NC}"
kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# 创建 Secret（如果提供了 auth key）
if [[ -n "$AUTH_KEY" ]]; then
    echo -e "${YELLOW}创建 Headscale auth key secret...${NC}"
    kubectl create secret generic ${RELEASE_NAME}-auth \
        --from-literal=auth-key="$AUTH_KEY" \
        -n $NAMESPACE \
        --dry-run=client -o yaml | kubectl apply -f -
fi

# 构建 Helm 命令
HELM_CMD="helm upgrade --install $RELEASE_NAME ./chart \
    --namespace $NAMESPACE \
    --set config.headscale.url=$HEADSCALE_URL \
    --set config.network.podCIDRBase=$POD_CIDR \
    --set config.network.serviceCIDR=$SERVICE_CIDR \
    --set config.ipam.type=$IPAM_TYPE"

# 如果提供了 auth key，添加到 Helm 命令中
if [[ -n "$AUTH_KEY" ]]; then
    HELM_CMD="$HELM_CMD --set config.headscale.authKey=$AUTH_KEY"
fi

echo -e "${YELLOW}部署 HeadCNI...${NC}"
echo "执行命令: $HELM_CMD"
eval $HELM_CMD

# 等待部署完成
echo -e "${YELLOW}等待部署完成...${NC}"
kubectl wait --for=condition=available --timeout=300s deployment/$RELEASE_NAME -n $NAMESPACE

# 检查 DaemonSet 状态
echo -e "${YELLOW}检查 DaemonSet 状态...${NC}"
kubectl get daemonset $RELEASE_NAME -n $NAMESPACE

# 显示 Pod 状态
echo -e "${YELLOW}显示 Pod 状态...${NC}"
kubectl get pods -l app.kubernetes.io/name=headcni -n $NAMESPACE

# 验证 CNI 配置
echo -e "${YELLOW}验证 CNI 配置...${NC}"
echo "检查节点上的 CNI 配置："
kubectl get nodes -o wide

echo ""
echo -e "${GREEN}=== 部署完成 ===${NC}"
echo ""
echo "下一步："
echo "1. 检查 CNI 插件是否正常工作："
echo "   kubectl get pods -l app.kubernetes.io/name=headcni -n $NAMESPACE"
echo ""
echo "2. 查看日志："
echo "   kubectl logs -l app.kubernetes.io/name=headcni -n $NAMESPACE"
echo ""
echo "3. 测试网络连接："
echo "   kubectl run test-pod --image=busybox --rm -it --restart=Never -- nslookup kubernetes.default"
echo ""
echo "4. 卸载（如果需要）："
echo "   helm uninstall $RELEASE_NAME -n $NAMESPACE" 