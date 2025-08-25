# HeadCNI

[中文](README_CN.md)

<p align="left"><img src="./logo.png" width="400" /></p>

---

## 🇺🇸 English

HeadCNI is a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality, providing a modular and extensible networking solution for Kubernetes clusters.

### 🚀 Features

- **Zero-Configuration Networking**: Automatic discovery and configuration of Tailscale networks
- **High Performance**: Efficient network forwarding based on veth pairs
- **Security**: Leverages Tailscale's WireGuard encryption
- **Simple Deployment**: No additional etcd cluster required
- **Monitoring Friendly**: Built-in Prometheus metrics
- **Daemon + Plugin Architecture**: Continuous daemon for dynamic network management
- **MagicDNS Support**: Native Tailscale DNS integration

### 📋 System Requirements

- Kubernetes 1.20+
- Tailscale client
- Headscale server
- Linux kernel 4.19+

### 🛠️ Quick Start

#### Method 1: Helm Deployment (Recommended)

```bash
# Clone the project
git clone <repository-url>
cd headcni

# Use deployment script
./deploy-with-helm.sh -u https://headscale.company.com -k YOUR_AUTH_KEY

# Or manually use Helm
helm upgrade --install headcni ./chart \
  --namespace kube-system \
  --set config.headscale.url=https://headscale.company.com \
  --set config.headscale.authKey=YOUR_AUTH_KEY \
  --set config.ipam.type=host-local
```

#### Method 2: Manual Deployment

##### 1. Build the Project

```bash
# Clone the project
git clone <repository-url>
cd headcni

# Build
make build
```

##### 2. Install CNI Plugin

```bash
# Install to system
make install

# Or install manually
sudo cp bin/headcni /opt/cni/bin/
sudo cp bin/headcni-daemon /opt/cni/bin/
sudo cp 10-headcni.conflist /etc/cni/net.d/
```

##### 3. Configure Tailscale

```bash
# Join Tailscale network
tailscale up --authkey=YOUR_AUTH_KEY
```

##### 4. Verify Installation

```bash
# Check CNI plugin
ls -la /opt/cni/bin/headcni

# Check configuration
cat /etc/cni/net.d/10-headcni.conflist
```

### ⚙️ Configuration

#### CNI Configuration File

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

#### MagicDNS Configuration

HeadCNI supports MagicDNS configuration for simplified DNS management:

```json
"magic_dns": {
  "enable": true,
  "base_domain": "cluster.local",
  "nameservers": ["10.2.0.1"],
  "search_domains": ["c.binrc.com"]
}
```

**MagicDNS Parameters:**
- **enable**: Enable MagicDNS functionality
- **base_domain**: Base domain for MagicDNS resolution
- **nameservers**: DNS server list
- **search_domains**: DNS search domain list

#### IPAM Types

HeadCNI supports two IPAM types:

1. **host-local**: Standard CNI IPAM plugin, simple and efficient

### 🔐 API Key Security

HeadCNI requires Headscale API Key for authentication. For security, **strongly avoid** storing API keys in plain text in configuration files.

#### Recommended: Environment Variables

```bash
# Direct environment variable
export HEADSCALE_API_KEY="your-api-key-here"
# Or
export HEADCNI_AUTH_KEY="your-api-key-here"

# From file
export HEADSCALE_API_KEY_FILE="/path/to/api-key-file"
# Or
export HEADCNI_AUTH_KEY_FILE="/path/to/api-key-file"
```

#### Kubernetes Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: headcni-auth
  namespace: kube-system
type: Opaque
data:
  api-key: <base64-encoded-api-key>
```

#### Environment Variable Priority

1. `HEADSCALE_API_KEY` environment variable
2. `HEADCNI_AUTH_KEY` environment variable
3. `HEADSCALE_API_KEY_FILE` file path
4. `HEADCNI_AUTH_KEY_FILE` file path
5. `auth_key` field in config file (not recommended)

### 🔧 Architecture

HeadCNI uses a **Daemon + Plugin** architecture:

- **CNI Plugin** (`headcni`): One-time execution for Pod network setup
- **Daemon** (`headcni-daemon`): Continuous running component for dynamic network management

#### Modes

1. **Host Mode**: Daemon uses existing host Tailscale interface
2. **Daemon Mode**: Daemon manages dedicated Tailscale interface (e.g., `headcni01`)

### 📊 Monitoring

HeadCNI provides Prometheus metrics:

- `headcni_ip_allocations_total`: Total IP allocations
- `headcni_ip_releases_total`: Total IP releases
- `headcni_network_errors_total`: Total network errors
- `headcni_pod_network_setup_duration_seconds`: Pod network setup duration

### 🐛 Troubleshooting

#### Common Issues

1. **CNI Plugin Cannot Load**
   ```bash
   # Check plugin permissions
   ls -la /opt/cni/bin/headcni
   
   # Check configuration
   cat /etc/cni/net.d/10-headcni.conflist
   ```

2. **IP Allocation Failure**
   ```bash
   # Check IPAM status
   journalctl -u kubelet | grep headcni
   
   # Check local storage
   ls -la /var/lib/headcni/
   ```

3. **Network Connectivity Issues**
   ```bash
   # Check Tailscale status
   tailscale status
   
   # Check network interfaces
   ip link show
   ```

### 🔧 Development

#### Project Structure

```
headcni/
├── cmd/                    # Command line tools
│   ├── headcni/           # Main CNI plugin
│   ├── headcni-daemon/    # Daemon component
│   └── cli/               # CLI tool
├── pkg/                   # Core packages
│   ├── daemon/           # Daemon logic
│   ├── headscale/        # Headscale client
│   ├── ipam/             # IP address management
│   ├── logging/          # Logging utilities
│   ├── monitoring/       # Monitoring server
│   └── networking/       # Network management
├── chart/                # Helm Chart
├── Dockerfile           # Container build
├── Makefile             # Build scripts
└── README.md            # Project documentation
```

#### Running Tests

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./pkg/ipam/...

# Run benchmark tests
go test -bench=. ./pkg/ipam/
```

#### Code Quality

```bash
# Format code
make fmt

# Static analysis
make vet

# Code linting
make lint
```

### 🎯 Use Cases

- **Hybrid Cloud**: Connect Kubernetes clusters across different cloud providers
- **Edge Computing**: Connect edge nodes with central clusters
- **Development Environment**: Quick setup of multi-cluster development environments
- **Disaster Recovery**: Cross-region cluster backup and recovery

### 🤝 Contributing

Issues and Pull Requests are welcome! Please follow these steps:

1. Fork the project
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

### 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

### 🔗 Related Links

- [CNI Specification](https://github.com/containernetworking/cni)
- [Tailscale Documentation](https://tailscale.com/kb/)
- [Headscale Documentation](https://github.com/juanfont/headscale)
- [Kubernetes Networking](https://kubernetes.io/docs/concepts/cluster-administration/networking/)
