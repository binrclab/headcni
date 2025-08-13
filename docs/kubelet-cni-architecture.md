# Kubelet 调用 HeadCNI 架构图

## 🔄 整体调用流程

```mermaid
sequenceDiagram
    participant Kubelet
    participant CNI Plugin
    participant IPAM Plugin
    participant Daemon
    participant Headscale
    participant Tailscale
    participant Kernel

    Note over Kubelet,Kernel: Pod 创建流程

    Kubelet->>CNI Plugin: ADD 命令 (stdin: config, args: container info)
    CNI Plugin->>CNI Plugin: loadConfig() - 解析 CNI 配置
    CNI Plugin->>CNI Plugin: parsePodInfo() - 解析 Pod 信息
    
    alt IPAM 类型
        CNI Plugin->>IPAM Plugin: AllocateIP(namespace, pod, containerID)
        IPAM Plugin->>IPAM Plugin: 选择分配策略 (sequential/random/dense-pack)
        IPAM Plugin-->>CNI Plugin: 返回 IP 地址和网关
    end
    
    CNI Plugin->>Kernel: 创建 veth 对 (CreateVethPair)
    CNI Plugin->>Kernel: 配置网络接口 (SetInterfaceIP, SetInterfaceMTU)
    CNI Plugin->>Kernel: 配置路由规则 (AddRoute)
    
    CNI Plugin->>Daemon: 通知 Pod 网络已创建
    Daemon->>Headscale: 请求路由通告 (RequestRoute)
    Daemon->>Tailscale: 配置 Tailscale 接口
    
    CNI Plugin-->>Kubelet: 返回网络配置结果
    Kubelet->>Kernel: 将 veth 一端移到容器网络命名空间
    
    Note over Kubelet,Kernel: Pod 删除流程
    
    Kubelet->>CNI Plugin: DEL 命令
    CNI Plugin->>IPAM Plugin: ReleaseIP(containerID)
    CNI Plugin->>Kernel: 删除网络接口和路由
    CNI Plugin->>Daemon: 通知 Pod 网络已删除
    Daemon->>Headscale: 清理路由通告
    CNI Plugin-->>Kubelet: 删除完成确认
```

## 🏗️ 详细组件架构

```mermaid
graph TB
    subgraph "Kubernetes 节点"
        subgraph "Kubelet 进程"
            Kubelet[Kubelet]
            CNI_Bridge[CNI Bridge]
            CNI_Config[CNI 配置管理]
        end
        
        subgraph "HeadCNI 组件"
            CNI_Plugin[headcni 插件]
            IPAM_Plugin[headcni-ipam 插件]
            Daemon[headcni-daemon]
        end
        
        subgraph "网络层"
            Veth_Pair[veth 对]
            Tailscale_Interface[Tailscale 接口]
            Routing[路由表]
        end
        
        subgraph "存储层"
            IPAM_Store[IPAM 存储]
            CNI_State[CNI 状态]
        end
    end
    
    subgraph "外部服务"
        Headscale[Headscale 服务器]
        Tailscale_Network[Tailscale 网络]
    end
    
    Kubelet --> CNI_Bridge
    CNI_Bridge --> CNI_Config
    CNI_Config --> CNI_Plugin
    CNI_Plugin --> IPAM_Plugin
    CNI_Plugin --> Daemon
    CNI_Plugin --> Veth_Pair
    CNI_Plugin --> Routing
    IPAM_Plugin --> IPAM_Store
    Daemon --> Headscale
    Daemon --> Tailscale_Interface
    Tailscale_Interface --> Tailscale_Network
    Veth_Pair --> CNI_State
```

## 📋 详细功能调用

### 1. **CNI 插件初始化**

```go
// 主要调用链
main() 
  ↓
skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("headcni"))
  ↓
loadConfig(args.StdinData, args.Args)
  ↓
parsePodInfo(args.Args)
```

**功能详情：**
- **loadConfig()**: 解析 CNI 配置文件，包括 MagicDNS、IPAM、MTU 等设置
- **parsePodInfo()**: 从 CNI 参数中提取 Pod 命名空间、名称、容器 ID 等信息

### 2. **IPAM 地址分配**

```go
// IPAM 调用链
plugin.ipamManager.AllocateIP(ctx, podInfo.Namespace, podInfo.Name, args.ContainerID)
  ↓
ipam.NewIPAMManager("headcni-daemon", subnet)
  ↓
manager.AllocateIP(ctx, namespace, podName, containerID)
  ↓
manager.selectAllocationStrategy() // sequential/random/dense-pack
  ↓
manager.allocateFromStrategy()
```

**功能详情：**
- **策略选择**: 根据配置选择 IP 分配策略
- **地址分配**: 从可用 IP 池中分配地址
- **状态记录**: 将分配结果记录到本地存储

### 3. **网络接口配置**

```go
// 网络配置调用链
plugin.setupPodNetwork(args, allocation)
  ↓
plugin.networkMgr.CreateVethPair(netnsPath, containerIfName, hostIfName)
  ↓
plugin.networkMgr.SetInterfaceIP(hostIfName, allocation.IP)
  ↓
plugin.networkMgr.SetInterfaceMTU(hostIfName, plugin.config.MTU)
  ↓
plugin.networkMgr.AddRoute(allocation.Gateway)
```

**功能详情：**
- **veth 创建**: 创建虚拟以太网设备对
- **IP 配置**: 为网络接口分配 IP 地址
- **MTU 设置**: 配置最大传输单元
- **路由配置**: 添加网络路由规则

### 4. **DNS 配置**

```go
// DNS 配置调用链
setupDNS(result, plugin.config.MagicDNS)
  ↓
populateDNSConfig(result.DNS, magicDNS)
  ↓
setNameservers(result.DNS, magicDNS.Nameservers)
  ↓
setSearchDomains(result.DNS, magicDNS.SearchDomains)
```

**功能详情：**
- **MagicDNS 启用**: 根据配置启用 MagicDNS
- **DNS 服务器**: 设置 DNS 服务器列表
- **搜索域**: 配置 DNS 搜索域名

### 5. **守护进程交互**

```go
// 守护进程通知
notifyDaemon(podInfo, allocation)
  ↓
sendPodNetworkEvent(event)
  ↓
daemon.handlePodEvent(event)
```

**功能详情：**
- **事件通知**: 向守护进程发送 Pod 网络事件
- **状态同步**: 保持 CNI 插件和守护进程状态一致

## 🔧 CNI 配置文件结构

```json
{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "headscale_url": "https://hs.binrc.com",
  "tailscale_socket": "/var/run/tailscale/tailscaled.sock",
  
  "pod_cidr": "10.244.0.0/24",
  "service_cidr": "10.96.0.0/16",
  
  "ipam": {
    "type": "host-local",
    "subnet": "10.244.0.0/24",
    "rangeStart": "10.244.0.10",
    "rangeEnd": "10.244.0.254",
    "gateway": "10.244.0.1"
  },
  
  "magic_dns": {
    "enable": true,
    "base_domain": "cluster.local",
    "nameservers": ["10.2.0.1"],
    "search_domains": ["c.binrc.com"]
  },
  
  "mtu": 1280,
  "enable_ipv6": false,
  "enable_network_policy": true
}
```

## 📊 调用时序图

```mermaid
sequenceDiagram
    participant Kubelet
    participant CNI
    participant IPAM
    participant Network
    participant Daemon
    participant Headscale

    Note over Kubelet,Headscale: Pod 创建 - ADD 操作
    
    Kubelet->>CNI: ADD(containerID, netns, ifName, args)
    CNI->>CNI: 解析配置和参数
    CNI->>IPAM: AllocateIP(namespace, pod, containerID)
    IPAM->>IPAM: 选择分配策略
    IPAM->>IPAM: 分配 IP 地址
    IPAM-->>CNI: 返回 IP 和网关
    
    CNI->>Network: CreateVethPair(netns, containerIf, hostIf)
    Network->>Network: 创建 veth 设备对
    CNI->>Network: SetInterfaceIP(hostIf, IP)
    CNI->>Network: SetInterfaceMTU(hostIf, MTU)
    CNI->>Network: AddRoute(gateway)
    
    CNI->>Daemon: 通知 Pod 网络创建
    Daemon->>Headscale: RequestRoute(podIP)
    Daemon-->>CNI: 确认路由配置
    
    CNI-->>Kubelet: 返回网络配置结果
    
    Note over Kubelet,Headscale: Pod 删除 - DEL 操作
    
    Kubelet->>CNI: DEL(containerID, netns, ifName, args)
    CNI->>IPAM: ReleaseIP(containerID)
    IPAM->>IPAM: 释放 IP 地址
    CNI->>Network: 删除网络接口
    CNI->>Daemon: 通知 Pod 网络删除
    Daemon->>Headscale: 清理路由
    CNI-->>Kubelet: 删除完成
```

## 🎯 关键功能点

### **CNI 插件核心功能**
1. **配置解析**: 解析 CNI 配置文件和参数
2. **IP 分配**: 通过 IPAM 插件分配 IP 地址
3. **网络配置**: 创建和配置网络接口
4. **路由设置**: 配置网络路由规则
5. **DNS 配置**: 设置 DNS 服务器和搜索域
6. **状态管理**: 维护网络配置状态

### **IPAM 插件功能**
1. **策略管理**: 支持多种 IP 分配策略
2. **地址池管理**: 管理可用 IP 地址池
3. **状态持久化**: 将分配状态保存到本地
4. **冲突检测**: 检测和避免 IP 地址冲突

### **守护进程功能**
1. **事件监听**: 监听 Pod 生命周期事件
2. **路由管理**: 与 Headscale 交互管理路由
3. **状态监控**: 监控网络接口状态
4. **故障恢复**: 处理网络故障和恢复

## 🔍 详细调用分析

### **Kubelet 调用 CNI 的完整流程**

```mermaid
graph TD
    A[Pod 调度到节点] --> B[Kubelet 创建容器]
    B --> C[Kubelet 调用 CNI 插件]
    C --> D[CNI 插件解析配置]
    D --> E[IPAM 分配 IP 地址]
    E --> F[创建网络接口]
    F --> G[配置路由和 DNS]
    G --> H[通知守护进程]
    H --> I[返回网络配置]
    I --> J[容器启动完成]
    
    K[Pod 删除] --> L[Kubelet 调用 CNI DEL]
    L --> M[释放 IP 地址]
    M --> N[删除网络接口]
    N --> O[清理路由]
    O --> P[通知守护进程清理]
    P --> Q[容器删除完成]
```

### **CNI 插件内部处理流程**

```mermaid
graph LR
    subgraph "CNI 插件处理"
        A[接收 ADD/DEL 命令] --> B[解析配置]
        B --> C[提取 Pod 信息]
        C --> D[调用 IPAM]
        D --> E[网络配置]
        E --> F[DNS 配置]
        F --> G[通知守护进程]
        G --> H[返回结果]
    end
    
    subgraph "IPAM 处理"
        I[接收分配请求] --> J[选择策略]
        J --> K[分配 IP]
        K --> L[记录状态]
        L --> M[返回结果]
    end
    
    subgraph "网络配置"
        N[创建 veth] --> O[配置 IP]
        O --> P[设置 MTU]
        P --> Q[添加路由]
    end
```

### **错误处理和重试机制**

```mermaid
sequenceDiagram
    participant Kubelet
    participant CNI
    participant IPAM
    participant Daemon

    Kubelet->>CNI: ADD 命令
    CNI->>IPAM: AllocateIP()
    
    alt IP 分配失败
        IPAM-->>CNI: 分配失败
        CNI->>CNI: 重试分配 (最多3次)
        CNI->>IPAM: 重新分配
        IPAM-->>CNI: 分配成功
    end
    
    CNI->>Daemon: 通知事件
    
    alt 守护进程不可用
        CNI->>CNI: 记录错误日志
        CNI->>CNI: 继续执行 (非阻塞)
    end
    
    CNI-->>Kubelet: 返回结果
```

## 📈 性能指标

### **关键性能指标**
- **IP 分配时间**: 从请求到分配完成的时间
- **网络配置时间**: veth 创建和配置的时间
- **DNS 配置时间**: DNS 服务器设置的时间
- **错误率**: 各种操作失败的比例
- **资源使用**: 内存和 CPU 使用情况

### **监控点**
- **CNI 插件执行时间**
- **IPAM 分配成功率**
- **网络接口创建成功率**
- **守护进程响应时间**
- **Headscale API 调用延迟**
