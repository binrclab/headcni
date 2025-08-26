# Dockerfile
# 多架构 HeadCNI 插件镜像构建文件

ARG TARGETARCH
ARG TARGETOS=linux

FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata curl

# 创建必要的目录
RUN mkdir -p /opt/cni/bin /etc/cni/net.d /var/lib/cni/headcni

# 根据目标架构下载对应的二进制文件
RUN case ${TARGETARCH} in \
        amd64) ARCH=amd64 ;; \
        arm64) ARCH=arm64 ;; \
        arm) ARCH=armv7 ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    echo "Downloading binary for ${TARGETOS}/${TARGETARCH} (${ARCH})" && \
    curl -L -o /tmp/headcni.tar.gz https://github.com/binrclab/headcni-plugin/releases/download/v1.0.0/headcni-${TARGETOS}-${ARCH}.tar.gz && \
    tar -xzf /tmp/headcni.tar.gz -C /opt/cni/bin && \
    rm /tmp/headcni.tar.gz && \
    chmod +x /opt/cni/bin/headcni

# 设置工作目录
WORKDIR /opt/cni

# 设置环境变量
ENV NODE_NAME=headcni-node
ENV CNI_PATH=/opt/cni/bin
ENV CNI_CONFDIR=/etc/cni/net.d

# 暴露端口
EXPOSE 8080

# 设置入口点
ENTRYPOINT ["/opt/cni/bin/headcni"]

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1 