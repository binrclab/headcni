# HeadCNI Makefile
# 用于构建、安装和管理 HeadCNI 系统

# 变量定义
PROJECT_NAME := github.com/binrc/headcni
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go 相关变量
GO := go
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GOFLAGS := -ldflags "-X github.com/binrclab/headcni/cmd/headcni-daemon/command.Version=$(VERSION) -X github.com/binrclab/headcni/cmd/headcni-daemon/command.BuildDate=$(BUILD_DATE) -X github.com/binrclab/headcni/cmd/headcni-daemon/command.GitCommit=$(GIT_COMMIT) -s -w"

# 目录定义
BIN_DIR := bin
BUILD_DIR := build
DIST_DIR := dist
SCRIPTS_DIR := scripts
EXAMPLES_DIR := examples
PKG_DIR := pkg

# 二进制文件
BINARIES := headcni headcni-ipam headcni-cli headcni-daemon
BIN_FILES := $(addprefix $(BIN_DIR)/,$(BINARIES))

# 安装路径
INSTALL_PREFIX := /usr/local
CNI_BIN_DIR := /opt/cni/bin
CNI_CONF_DIR := /etc/cni/net.d
SYSTEMD_DIR := /etc/systemd/system
CONFIG_DIR := /etc/headcni
DATA_DIR := /var/lib/cni/headcni

# 服务名称
SERVICE_NAME := headcni-daemon

# 默认目标
.PHONY: all
all: build

# 显示帮助信息
.PHONY: help
help:
	@echo "HeadCNI Makefile 帮助"
	@echo "=================="
	@echo ""
	@echo "构建目标:"
	@echo "  build          - 构建核心组件 (headcni + headcni-ipam)"
	@echo "  build-ipam     - 构建 headcni-ipam"
	@echo "  build-main     - 构建主 CNI 插件"
	@echo "  build-daemon   - 构建 headcni-daemon (可选)"
	@echo "  build-daemon   - 构建 headcni-daemon (可选)"
	@echo "  clean          - 清理构建文件"
	@echo ""
	@echo "安装目标:"
	@echo "  install        - 安装核心组件到系统"
	@echo "  install-cni    - 安装 CNI 插件"
	@echo "  install-daemon - 安装 headcni-daemon 服务 (可选)"
	@echo "  uninstall      - 卸载所有组件"
	@echo ""
	@echo "服务管理:"
	@echo "  start          - 启动 headcni-daemon 服务"
	@echo "  stop           - 停止 headcni-daemon 服务"
	@echo "  restart        - 重启 headcni-daemon 服务"
	@echo "  status         - 查看服务状态"
	@echo "  logs           - 查看服务日志"
	@echo ""
	@echo "部署目标:"
	@echo "  deploy         - 完整部署 (构建 + 安装 + 启动)"
	@echo "  deploy-script  - 使用部署脚本部署"
	@echo "  verify         - 验证部署"
	@echo ""
	@echo "开发目标:"
	@echo "  test           - 运行测试"
	@echo "  lint           - 代码检查"
	@echo "  fmt            - 格式化代码"
	@echo "  vet            - 代码静态分析"
	@echo ""
	@echo "维护目标:"
	@echo "  backup         - 备份配置"
	@echo "  restore        - 恢复配置"
	@echo "  upgrade        - 升级系统"
	@echo "  package        - 打包发布"
	@echo ""
	@echo "Docker 目标:"
	@echo "  docker         - 构建 Docker 镜像"
	@echo "  docker-pull    - 拉取 Docker 镜像"
	@echo "  docker-login   - Docker Hub 认证"
	@echo "  docker-push    - 推送 Docker 镜像到注册表 (需要认证)"
	@echo "  docker-clean   - 清理 Docker 镜像"
	@echo ""
	@echo "变量:"
	@echo "  VERSION=$(VERSION)"
	@echo "  BUILD_TIME=$(BUILD_TIME)"
	@echo "  GIT_COMMIT=$(GIT_COMMIT)"
	@echo "  DOCKER_USERNAME - Docker Hub 用户名 (用于认证)"
	@echo "  DOCKER_PASSWORD - Docker Hub 密码 (用于认证)"
	@echo "  DOCKER_TOKEN    - Docker Hub 访问令牌 (用于认证)"

# 创建必要的目录
$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

# 构建目标
.PHONY: build
build: $(BIN_DIR) $(BIN_FILES)
	@echo "构建完成: $(BIN_FILES)"

.PHONY: build-ipam
build-ipam: $(BIN_DIR)
	@echo "构建 headcni-ipam..."
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/headcni-ipam ./cmd/headcni-ipam/

.PHONY: build-main
build-main: $(BIN_DIR)
	@echo "构建主 CNI 插件..."
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/headcni ./cmd/headcni/

# 可选组件构建目标
.PHONY: build-daemon
build-daemon: $(BIN_DIR)
	@echo "构建 headcni-daemon..."
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/headcni-daemon ./cmd/headcni-daemon/

.PHONY: build-cli
build-cli: $(BIN_DIR)
	@echo "构建 headcni-cli..."
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/headcni-cli ./cmd/cli/

# 二进制文件依赖
$(BIN_DIR)/headcni-ipam: build-ipam
$(BIN_DIR)/headcni: build-main
$(BIN_DIR)/headcni-daemon: build-daemon
$(BIN_DIR)/headcni-cli: build-cli

# 清理目标
.PHONY: clean
clean:
	@echo "清理构建文件..."
	rm -rf $(BIN_DIR)
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)
	$(GO) clean -cache

# 安装目标
.PHONY: install
install: install-cni install-config
	@echo "安装完成"

.PHONY: install-cni
install-cni: build
	@echo "安装 CNI 插件..."
	sudo mkdir -p $(CNI_BIN_DIR)
	sudo mkdir -p $(CNI_CONF_DIR)
	sudo cp $(BIN_DIR)/headcni $(CNI_BIN_DIR)/
	sudo cp $(BIN_DIR)/headcni-ipam $(CNI_BIN_DIR)/
	sudo cp $(BIN_DIR)/headcni-daemon $(CNI_BIN_DIR)/
	sudo chmod +x $(CNI_BIN_DIR)/headcni
	sudo chmod +x $(CNI_BIN_DIR)/headcni-ipam
	sudo chmod +x $(CNI_BIN_DIR)/headcni-daemon
	@echo "CLI 工具已构建，可以运行: ./bin/headcni-cli --help"
	@if [ -f $(EXAMPLES_DIR)/cni-config.json ]; then \
		sudo cp $(EXAMPLES_DIR)/cni-config.json $(CNI_CONF_DIR)/10-headcni.conf; \
	else \
		echo "警告: 未找到 CNI 配置文件，请手动创建"; \
	fi
	@echo "CNI 插件安装完成"

.PHONY: install-daemon
install-daemon: build
	@echo "安装 headcni-daemon 服务..."
	sudo mkdir -p $(CONFIG_DIR)
	sudo mkdir -p $(DATA_DIR)
	@if [ ! -f $(CONFIG_DIR)/auth-key ]; then \
		echo "请创建认证密钥文件: sudo tee $(CONFIG_DIR)/auth-key > /dev/null"; \
		echo "然后设置权限: sudo chmod 600 $(CONFIG_DIR)/auth-key"; \
	fi
	@echo "创建 systemd 服务文件..."
	@echo "[Unit]" > /tmp/headcni-daemon.service
	@echo "Description=HeadCNI Daemon Service" >> /tmp/headcni-daemon.service
	@echo "After=network.target" >> /tmp/headcni-daemon.service
	@echo "" >> /tmp/headcni-daemon.service
	@echo "[Service]" >> /tmp/headcni-daemon.service
	@echo "Type=simple" >> /tmp/headcni-daemon.service
	@echo "User=root" >> /tmp/headcni-daemon.service
	@echo "ExecStart=$(shell pwd)/$(BIN_DIR)/headcni-daemon --headscale-url=http://localhost:50443 --mode=host --auth-key-file=$(CONFIG_DIR)/auth-key" >> /tmp/headcni-daemon.service
	@echo "Restart=always" >> /tmp/headcni-daemon.service
	@echo "RestartSec=5" >> /tmp/headcni-daemon.service
	@echo "Environment=NODE_NAME=$$(hostname)" >> /tmp/headcni-daemon.service
	@echo "" >> /tmp/headcni-daemon.service
	@echo "[Install]" >> /tmp/headcni-daemon.service
	@echo "WantedBy=multi-user.target" >> /tmp/headcni-daemon.service
	sudo cp /tmp/headcni-daemon.service $(SYSTEMD_DIR)/
	sudo systemctl daemon-reload
	sudo systemctl enable $(SERVICE_NAME)
	@echo "headcni-daemon 服务安装完成"

.PHONY: install-config
install-config:
	@echo "安装配置文件..."
	sudo mkdir -p $(CONFIG_DIR)
	@echo "配置文件安装完成"

# 卸载目标
.PHONY: uninstall
uninstall:
	@echo "卸载 HeadCNI..."
	sudo systemctl stop $(SERVICE_NAME) 2>/dev/null || true
	sudo systemctl disable $(SERVICE_NAME) 2>/dev/null || true
	sudo rm -f $(SYSTEMD_DIR)/$(SERVICE_NAME).service
	sudo systemctl daemon-reload
	sudo rm -f $(CNI_BIN_DIR)/headcni*
	sudo rm -f $(CNI_CONF_DIR)/10-headcni.conf
	sudo rm -rf $(CONFIG_DIR)
	sudo rm -rf $(DATA_DIR)
	@echo "卸载完成"

# 服务管理目标
.PHONY: start
start:
	@echo "启动 $(SERVICE_NAME) 服务..."
	sudo systemctl start $(SERVICE_NAME)
	sudo systemctl status $(SERVICE_NAME)

.PHONY: stop
stop:
	@echo "停止 $(SERVICE_NAME) 服务..."
	sudo systemctl stop $(SERVICE_NAME)

.PHONY: restart
restart:
	@echo "重启 $(SERVICE_NAME) 服务..."
	sudo systemctl restart $(SERVICE_NAME)
	sudo systemctl status $(SERVICE_NAME)

.PHONY: status
status:
	@echo "查看 $(SERVICE_NAME) 服务状态..."
	sudo systemctl status $(SERVICE_NAME)

.PHONY: logs
logs:
	@echo "查看 $(SERVICE_NAME) 服务日志..."
	sudo journalctl -u $(SERVICE_NAME) -f

# 部署目标
.PHONY: deploy
deploy: build install start verify
	@echo "部署完成"

.PHONY: deploy-script
deploy-script:
	@echo "使用部署脚本部署..."
	sudo $(SCRIPTS_DIR)/deploy-headcni.sh

.PHONY: verify
verify:
	@echo "验证部署..."
	@echo "检查 CNI 插件..."
	@if [ -f "$(CNI_BIN_DIR)/headcni" ] && [ -f "$(CNI_BIN_DIR)/headcni-ipam" ]; then \
		echo "✓ CNI 插件安装正确"; \
	else \
		echo "✗ CNI 插件安装失败"; \
		exit 1; \
	fi
	@echo "检查配置文件..."
	@if [ -f "$(CNI_CONF_DIR)/10-headcni.conf" ]; then \
		echo "✓ CNI 配置文件存在"; \
	else \
		echo "✗ CNI 配置文件不存在"; \
		exit 1; \
	fi
	@echo "检查服务状态..."
	@if sudo systemctl is-active --quiet $(SERVICE_NAME); then \
		echo "✓ $(SERVICE_NAME) 服务运行正常"; \
	else \
		echo "✗ $(SERVICE_NAME) 服务未运行"; \
		exit 1; \
	fi
	@echo "测试 API..."
	@if curl -s http://localhost:8080/healthz > /dev/null; then \
		echo "✓ API 健康检查通过"; \
	else \
		echo "⚠ API 健康检查失败，服务可能还在启动中"; \
	fi
	@echo "验证完成"

# 开发目标
.PHONY: test
test:
	@echo "运行测试..."
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage:
	@echo "运行测试并生成覆盖率报告..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

.PHONY: lint
lint:
	@echo "代码检查..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint 未安装，跳过代码检查"; \
	fi

.PHONY: fmt
fmt:
	@echo "格式化代码..."
	$(GO) fmt ./...

.PHONY: vet
vet:
	@echo "代码静态分析..."
	$(GO) vet ./...

# 维护目标
.PHONY: backup
backup:
	@echo "备份配置..."
	@BACKUP_FILE="headcni-backup-$$(date +%Y%m%d-%H%M%S).tar.gz"; \
	sudo tar -czf "$$BACKUP_FILE" \
		$(CNI_CONF_DIR)/10-headcni.conf \
		$(SYSTEMD_DIR)/$(SERVICE_NAME).service \
		$(CONFIG_DIR)/ \
		$(DATA_DIR)/ 2>/dev/null || true; \
	echo "备份文件: $$BACKUP_FILE"

.PHONY: restore
restore:
	@echo "恢复配置..."
	@read -p "请输入备份文件路径: " backup_file; \
	if [ -f "$$backup_file" ]; then \
		sudo tar -xzf "$$backup_file" -C /; \
		sudo systemctl daemon-reload; \
		sudo systemctl restart $(SERVICE_NAME); \
		echo "配置恢复完成"; \
	else \
		echo "备份文件不存在: $$backup_file"; \
		exit 1; \
	fi

.PHONY: upgrade
upgrade: backup
	@echo "升级 HeadCNI..."
	git pull
	make build
	sudo systemctl stop $(SERVICE_NAME)
	make install
	sudo systemctl start $(SERVICE_NAME)
	make verify
	@echo "升级完成"

.PHONY: package
package: build $(DIST_DIR)
	@echo "打包发布..."
	@PACKAGE_NAME="$(PROJECT_NAME)-$(VERSION)-$(GOOS)-$(GOARCH)"; \
	mkdir -p $(DIST_DIR)/$$PACKAGE_NAME; \
	cp -r $(BIN_DIR)/* $(DIST_DIR)/$$PACKAGE_NAME/; \
	cp -r $(EXAMPLES_DIR) $(DIST_DIR)/$$PACKAGE_NAME/; \
	cp -r $(SCRIPTS_DIR) $(DIST_DIR)/$$PACKAGE_NAME/; \
	cp README.md $(DIST_DIR)/$$PACKAGE_NAME/; \
	cp DEPLOYMENT.md $(DIST_DIR)/$$PACKAGE_NAME/; \
	cp Makefile $(DIST_DIR)/$$PACKAGE_NAME/; \
	cd $(DIST_DIR) && tar -czf $$PACKAGE_NAME.tar.gz $$PACKAGE_NAME; \
	rm -rf $(DIST_DIR)/$$PACKAGE_NAME; \
	echo "发布包: $(DIST_DIR)/$$PACKAGE_NAME.tar.gz"

# 调试目标
.PHONY: debug
debug:
	@echo "调试信息:"
	@echo "  VERSION: $(VERSION)"
	@echo "  BUILD_TIME: $(BUILD_TIME)"
	@echo "  GIT_COMMIT: $(GIT_COMMIT)"
	@echo "  GOOS: $(GOOS)"
	@echo "  GOARCH: $(GOARCH)"
	@echo "  BIN_DIR: $(BIN_DIR)"
	@echo "  CNI_BIN_DIR: $(CNI_BIN_DIR)"
	@echo "  CNI_CONF_DIR: $(CNI_CONF_DIR)"

# 检查依赖
.PHONY: check-deps
check-deps:
	@echo "检查依赖..."
	@if ! command -v go > /dev/null; then \
		echo "✗ Go 未安装"; \
		exit 1; \
	else \
		echo "✓ Go 已安装: $$(go version)"; \
	fi
	@if ! command -v tailscale > /dev/null; then \
		echo "⚠ Tailscale 未安装"; \
	else \
		echo "✓ Tailscale 已安装"; \
	fi
	@echo "依赖检查完成"

# 初始化开发环境
.PHONY: init-dev
init-dev: check-deps
	@echo "初始化开发环境..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "开发环境初始化完成"

# 生成文档
.PHONY: docs
docs:
	@echo "生成文档..."
	@if command -v godoc > /dev/null; then \
		echo "启动 godoc 服务器: http://localhost:6060"; \
		godoc -http=:6060; \
	else \
		echo "godoc 未安装，跳过文档生成"; \
	fi

# 清理所有内容
.PHONY: clean-all
clean-all: clean
	@echo "清理所有内容..."
	sudo rm -rf $(CNI_BIN_DIR)/headcni*
	sudo rm -rf $(CNI_CONF_DIR)/10-headcni.conf
	sudo rm -rf $(SYSTEMD_DIR)/$(SERVICE_NAME).service
	sudo rm -rf $(CONFIG_DIR)
	sudo rm -rf $(DATA_DIR)
	@echo "清理完成"

# Docker 相关目标
.PHONY: docker
docker: 
	@echo "构建 Docker 镜像..."
	docker build -t binrc/headcni:$(VERSION) .
	docker tag binrc/headcni:$(VERSION) binrc/headcni:latest
	@echo "Docker 镜像构建完成: binrc/headcni:$(VERSION), binrc/headcni:latest"

.PHONY: docker-pull
docker-pull:
	@echo "拉取 Docker 镜像..."
	docker pull binrc/headcni:$(VERSION)
	docker pull binrc/headcni:latest
	@echo "Docker 镜像拉取完成: binrc/headcni:$(VERSION), binrc/headcni:latest"

.PHONY: docker-push
docker-push: docker
	@echo "推送 Docker 镜像..."
	@echo "注意: 推送需要 Docker Hub 认证"
	@echo "请确保已登录: docker login"
	@echo "或者使用: docker login -u YOUR_USERNAME -p YOUR_PASSWORD"
	@echo ""
	@if ! docker info > /dev/null 2>&1; then \
		echo "错误: Docker 未运行或无法连接"; \
		exit 1; \
	fi
	@if ! docker images | grep -q "binrc/headcni"; then \
		echo "错误: 未找到 binrc/headcni 镜像，请先运行 make docker"; \
		exit 1; \
	fi
	docker push binrc/headcni:$(VERSION)
	docker push binrc/headcni:latest
	@echo "Docker 镜像推送完成: binrc/headcni:$(VERSION), binrc/headcni:latest"

.PHONY: docker-login
docker-login:
	@echo "Docker Hub 认证..."
	@if [ -n "$(DOCKER_USERNAME)" ] && [ -n "$(DOCKER_PASSWORD)" ]; then \
		echo "使用环境变量进行认证..."; \
		docker login -u "$(DOCKER_USERNAME)" -p "$(DOCKER_PASSWORD)"; \
	elif [ -n "$(DOCKER_TOKEN)" ]; then \
		echo "使用 Docker 令牌进行认证..."; \
		echo "$(DOCKER_TOKEN)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin; \
	else \
		echo "请输入您的 Docker Hub 用户名和密码:"; \
		read -p "用户名: " username; \
		read -s -p "密码: " password; \
		echo ""; \
		docker login -u "$$username" -p "$$password"; \
	fi
	@echo "认证完成"

.PHONY: docker-clean
docker-clean:
	@echo "清理 Docker 镜像..."
	docker rmi binrc/headcni:$(VERSION) binrc/headcni:latest 2>/dev/null || true
	@echo "Docker 镜像清理完成"

# 显示版本信息
.PHONY: version
version:
	@echo "HeadCNI $(VERSION)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Go Version: $(shell go version)"
	@echo "OS/Arch: $(GOOS)/$(GOARCH)"

# 测试命令功能
.PHONY: test-commands
test-commands: build
	@echo "测试命令功能..."
	@echo "1. 测试版本命令:"
	./$(BIN_DIR)/headcni-daemon version
	@echo ""
	@echo "2. 测试健康检查命令:"
	./$(BIN_DIR)/headcni-daemon health --timeout=10s
	@echo ""
	@echo "3. 测试配置显示命令:"
	./$(BIN_DIR)/headcni-daemon config show
	@echo ""
	@echo "4. 测试帮助命令:"
	./$(BIN_DIR)/headcni-daemon --help

# 运行修复后的命令
.PHONY: run-version
run-version: build
	@echo "运行版本命令:"
	./$(BIN_DIR)/headcni-daemon version

.PHONY: run-health
run-health: build
	@echo "运行健康检查:"
	./$(BIN_DIR)/headcni-daemon health --timeout=30s

.PHONY: run-config-validate
run-config-validate: build
	@echo "运行配置验证:"
	@if [ -f chart/values.yaml ]; then \
		./$(BIN_DIR)/headcni-daemon config validate --config=chart/values.yaml; \
	else \
		echo "配置文件 chart/values.yaml 不存在，跳过验证"; \
	fi

.PHONY: run-config-show
run-config-show: build
	@echo "运行配置显示:"
	./$(BIN_DIR)/headcni-daemon config show 