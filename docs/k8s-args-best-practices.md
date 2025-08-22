# Kubernetes 应用命令行参数处理最佳实践

## 概述

在 Kubernetes 中，应用需要灵活地处理配置参数。本文档总结了常见的参数传递方式和最佳实践。

## 参数传递方式

### 1. 环境变量 + ConfigMap（推荐）

**优点：**
- 简单直观
- 易于调试
- 支持动态更新（需要重启 Pod）
- 符合 12-Factor App 原则

**适用场景：**
- 大多数配置参数
- 非敏感信息
- 需要频繁调整的参数

**示例：**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  DATABASE_URL: "postgresql://localhost:5432/mydb"
  LOG_LEVEL: "info"
  API_PORT: "8080"
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        env:
        - name: DATABASE_URL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: DATABASE_URL
```

### 2. 配置文件 + ConfigMap

**优点：**
- 支持复杂配置结构
- 易于版本控制
- 支持配置验证

**适用场景：**
- 复杂配置结构
- 需要配置验证
- 配置项较多

**示例：**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  config.yaml: |
    database:
      host: localhost
      port: 5432
    logging:
      level: info
      format: json
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        command: ["/app/myapp"]
        args: ["--config=/etc/app/config/config.yaml"]
        volumeMounts:
        - name: config-volume
          mountPath: /etc/app/config
      volumes:
      - name: config-volume
        configMap:
          name: app-config
```

### 3. 命令行参数

**优点：**
- 启动时确定
- 性能好
- 易于调试

**适用场景：**
- 启动时确定的参数
- 性能敏感的参数
- 调试参数

**示例：**
```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        command: ["/app/myapp"]
        args:
        - "--port=8080"
        - "--log-level=info"
        - "--debug=false"
```

### 4. 混合方式

**优点：**
- 灵活性高
- 可以根据参数特性选择合适的方式

**适用场景：**
- 复杂应用
- 不同参数有不同的更新频率
- 需要平衡灵活性和性能

**示例：**
```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        command: ["/app/myapp"]
        args:
        - "--config=/etc/app/config/app.yaml"
        - "--port=$(API_PORT)"
        env:
        - name: API_PORT
          value: "8080"
        - name: DATABASE_URL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: DATABASE_URL
```

## 敏感信息处理

### 使用 Secret

**最佳实践：**
- 敏感信息使用 Secret 存储
- 避免在 ConfigMap 中存储敏感信息
- 使用 RBAC 控制 Secret 访问权限

**示例：**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secret
type: Opaque
data:
  database-password: cGFzc3dvcmQ=  # base64 encoded
  api-key: YXBpLWtleQ==
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        env:
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: app-secret
              key: database-password
```

## 动态配置

### 1. 使用 Init Container

**适用场景：**
- 需要根据环境动态生成配置
- 配置依赖于其他服务
- 复杂的配置逻辑

**示例：**
```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      initContainers:
      - name: config-generator
        image: busybox:latest
        command: ["/bin/sh"]
        args:
        - -c
        - |
          cat > /tmp/config/app.yaml << EOF
          database:
            url: ${DATABASE_URL}
          logging:
            level: ${LOG_LEVEL}
          EOF
        env:
        - name: DATABASE_URL
          value: "postgresql://localhost:5432/mydb"
        - name: LOG_LEVEL
          value: "info"
        volumeMounts:
        - name: config-volume
          mountPath: /tmp/config
      containers:
      - name: app
        image: myapp:latest
        command: ["/app/myapp"]
        args: ["--config=/etc/app/config/app.yaml"]
        volumeMounts:
        - name: config-volume
          mountPath: /etc/app/config
      volumes:
      - name: config-volume
        emptyDir: {}
```

### 2. 使用 Operator 模式

**适用场景：**
- 复杂的配置管理
- 需要跨多个资源协调
- 配置依赖于集群状态

## Helm 集成

### 使用 Helm 模板

**优点：**
- 配置参数化
- 支持多环境部署
- 版本管理

**示例：**
```yaml
# values.yaml
config:
  database:
    url: "postgresql://localhost:5432/mydb"
  logging:
    level: "info"
  server:
    port: 8080

# templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
        command: ["/app/myapp"]
        args:
        - "--config=/etc/app/config/app.yaml"
        - "--port={{ .Values.config.server.port }}"
        env:
        - name: LOG_LEVEL
          value: {{ .Values.config.logging.level | quote }}
```

## 最佳实践总结

### 1. 参数分类

- **环境变量**：运行时配置，非敏感信息
- **Secret**：敏感信息（密码、密钥等）
- **命令行参数**：启动时确定的参数
- **配置文件**：复杂配置结构

### 2. 优先级顺序

1. 命令行参数（最高优先级）
2. 环境变量
3. 配置文件
4. 默认值（最低优先级）

### 3. 安全考虑

- 敏感信息使用 Secret
- 避免在日志中输出敏感信息
- 使用 RBAC 控制访问权限
- 定期轮换密钥

### 4. 可维护性

- 使用有意义的参数名称
- 提供默认值
- 添加参数验证
- 记录参数文档

### 5. 调试友好

- 提供调试模式
- 支持参数验证
- 记录配置加载过程
- 提供配置查看命令

## HeadCNI 的实现

HeadCNI 采用了混合方式：

1. **配置文件**：基础配置（`daemon.yaml`）
2. **环境变量**：运行时配置（从 ConfigMap 获取）
3. **命令行参数**：调试和覆盖参数
4. **Secret**：敏感信息（HeadScale Auth Key）

这种设计提供了最大的灵活性，同时保持了良好的可维护性。 