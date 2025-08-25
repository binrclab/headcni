# 构建阶段
FROM golang:1.24-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git make

# 设置 Go 代理为中国镜像
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn

# 设置工作目录
WORKDIR /app

# 复制源代码（包括 .git 目录）
COPY . .

# #proxy
# RUN export HTTP_PROXY=http://192.168.6.185:7897 && export HTTPS_PROXY=http://192.168.6.185:7897

# 强制更新到指定分支
RUN git submodule update --init --recursive --remote

# 强制切换到微调分支
RUN cd tailscale && \
    git checkout v1.86.4.fine-tuned-version && \
    echo "=== Current branch: $(git branch --show-current)" && \
    echo "=== Current commit: $(git rev-parse HEAD)"

# 验证子模块分支和内容
RUN cd tailscale && git branch -a && git log --oneline -5 && ls -la && ls -la cmd/

# 构建二进制文件
RUN make build

# 构建子模块中的 Tailscale
RUN cd tailscale && \
    BRANCH_NAME=$(git branch --show-current || git name-rev --name-only HEAD | sed 's/remotes\/origin\///') && \
    echo "=== Building from branch: $BRANCH_NAME" && \
    echo "=== Commit hash: $(git rev-parse HEAD)" && \
    echo "=== Remote branches: $(git branch -r | grep fine-tuned)" && \
    echo "=== Injecting version info..." && \
    go build -ldflags "-X tailscale.com/version.shortStamp=$BRANCH_NAME -X tailscale.com/version.longStamp=$BRANCH_NAME -X tailscale.com/version.gitCommitStamp=$(git rev-parse HEAD)" -o /app/bin/tailscale ./cmd/tailscale && \
    go build -ldflags "-X tailscale.com/version.shortStamp=$BRANCH_NAME -X tailscale.com/version.longStamp=$BRANCH_NAME -X tailscale.com/version.gitCommitStamp=$(git rev-parse HEAD)" -o /app/bin/tailscaled ./cmd/tailscaled && \
    ls -la /app/bin/ && \
    echo "Tailscale binaries built from submodule source"

# 运行时阶段
FROM alpine:3.19

# 安装必要的工具
RUN apk add --no-cache \
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

# 从构建阶段复制 Tailscale 二进制文件
COPY --from=builder /app/bin/tailscale /usr/local/bin/
COPY --from=builder /app/bin/tailscaled /usr/local/bin/

# 设置执行权限
RUN chmod +x /opt/cni/bin/*

# 设置环境变量
ENV PATH="/opt/cni/bin:$PATH"

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# 默认命令
CMD ["/opt/cni/bin/headcni-daemon"] 