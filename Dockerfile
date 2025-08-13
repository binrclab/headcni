# 构建阶段
FROM golang:1.24-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git make

# 设置工作目录
WORKDIR /app

# 复制源代码
COPY . .

# 构建二进制文件
RUN make build

# 运行时阶段
FROM alpine:3.19

# 安装 Tailscale 和必要的工具
RUN apk add --no-cache \
    tailscale \
    iptables \
    iproute2 \
    net-tools \
    curl \
    ca-certificates \
    && rm -rf /var/cache/apk/*

# 创建必要的目录
RUN mkdir -p \
    /opt/cni/bin \
    /etc/cni/net.d \
    /var/lib/headcni \
    /var/run/headcni \
    /var/run/headcni/tailscale \
    /var/lib/headcni/tailscale

# 从构建阶段复制二进制文件
COPY --from=builder /app/bin/headcni /opt/cni/bin/
COPY --from=builder /app/bin/headcni-ipam /opt/cni/bin/
COPY --from=builder /app/bin/headcni-daemon /opt/cni/bin/
COPY --from=builder /app/bin/headcni-cli /opt/cni/bin/

# 设置执行权限
RUN chmod +x /opt/cni/bin/*

# 复制配置文件
COPY chart/templates/configmap.yaml /etc/cni/net.d/10-headcni.conflist

# 设置环境变量
ENV PATH="/opt/cni/bin:$PATH"

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# 默认命令
CMD ["/opt/cni/bin/headcni-daemon"] 