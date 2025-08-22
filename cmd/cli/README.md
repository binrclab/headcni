# HeadCNI CLI Tool

A comprehensive command-line interface for managing HeadCNI, a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality.

## Features

- **Installation Management**: Install, upgrade, and uninstall HeadCNI
- **Status Monitoring**: Real-time status checking and health monitoring
- **Connectivity Testing**: Comprehensive network connectivity tests
- **Log Management**: View and filter logs from HeadCNI components
- **Metrics Collection**: View Prometheus metrics from HeadCNI daemon
- **Diagnostics**: Collect comprehensive diagnostic information
- **Backup & Restore**: Backup and restore HeadCNI configuration
- **Shell Completion**: Generate completion scripts for various shells

## Installation

### Prerequisites

- Kubernetes cluster (1.20+)
- kubectl configured and accessible
- Tailscale client installed
- Headscale server accessible

### Building from Source

```bash
# Clone the repository
git clone https://github.com/binrclab/headcni.git
cd headcni

# Build the CLI tool
make build-cli

# Install to your PATH
sudo cp bin/headcni /usr/local/bin/
```

## Commands

### Core Commands

#### `headcni install`
Install HeadCNI to your Kubernetes cluster.

```bash
# Basic installation
headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY

# Custom configuration
headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY \
  --pod-cidr 10.42.0.0/16 --ipam-type headcni-ipam

# Dry run
headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY --dry-run
```

#### `headcni status`
Check the status of HeadCNI deployment.

```bash
# Basic status check
headcni status

# Status with logs
headcni status --show-logs

# JSON output
headcni status --output json

# Custom namespace
headcni status --namespace my-namespace
```

#### `headcni connect-test`
Test network connectivity in your HeadCNI cluster.

```bash
# Basic connectivity test
headcni connect-test

# Verbose test with custom timeout
headcni connect-test --verbose --timeout 60

# Test in specific namespace
headcni connect-test --namespace my-namespace
```

#### `headcni upgrade`
Upgrade HeadCNI to a newer version.

```bash
# Upgrade to latest version
headcni upgrade

# Upgrade to specific version
headcni upgrade --image-tag v1.1.0

# Dry run upgrade
headcni upgrade --dry-run

# Force upgrade (skip compatibility checks)
headcni upgrade --force
```

### Monitoring Commands

#### `headcni logs`
View logs from HeadCNI components.

```bash
# View logs from all HeadCNI pods
headcni logs

# Follow logs from a specific pod
headcni logs headcni-abc123 --follow

# View last 100 lines with timestamps
headcni logs --tail 100 --timestamps

# View logs from a specific container
headcni logs --container headcni-daemon

# View logs since a specific time
headcni logs --since 1h
```

#### `headcni metrics`
View Prometheus metrics from HeadCNI.

```bash
# View all metrics
headcni metrics

# View specific metrics
headcni metrics --filter "headcni_ip"

# Export metrics to JSON
headcni metrics --output json

# Use custom port
headcni metrics --port 9090
```

### Maintenance Commands

#### `headcni diagnostics`
Collect diagnostic information for troubleshooting.

```bash
# Basic diagnostics
headcni diagnostics

# Include logs and YAML manifests
headcni diagnostics --include-logs --include-yaml

# Save to specific directory
headcni diagnostics --output-dir ./diagnostics

# Verbose output
headcni diagnostics --verbose
```

#### `headcni backup`
Backup HeadCNI configuration and resources.

```bash
# Basic backup
headcni backup

# Backup with logs
headcni backup --include-logs

# Backup to specific file
headcni backup --output-file headcni-backup.json

# Verbose backup
headcni backup --verbose
```

#### `headcni restore`
Restore HeadCNI configuration from backup.

```bash
# Restore from backup file
headcni restore --input-file headcni-backup.json

# Dry run restore
headcni restore --input-file headcni-backup.json --dry-run

# Force restore (skip checks)
headcni restore --input-file headcni-backup.json --force
```

### Utility Commands

#### `headcni config`
Manage HeadCNI configuration.

```bash
# Show current configuration
headcni config show

# Validate configuration
headcni config validate

# Generate sample configuration
headcni config generate
```

#### `headcni uninstall`
Remove HeadCNI from your cluster.

```bash
# Basic uninstall
headcni uninstall

# Dry run uninstall
headcni uninstall --dry-run

# Force uninstall
headcni uninstall --force
```

#### `headcni completion`
Generate shell completion scripts.

```bash
# Generate bash completion
headcni completion bash

# Generate zsh completion
headcni completion zsh

# Generate fish completion
headcni completion fish

# Generate PowerShell completion
headcni completion powershell
```

## Configuration

### Environment Variables

The CLI tool respects the following environment variables:

- `HEADCNI_NAMESPACE`: Default namespace (default: kube-system)
- `HEADCNI_RELEASE_NAME`: Default release name (default: headcni)
- `KUBECONFIG`: Path to kubeconfig file
- `HEADSCALE_API_KEY`: Headscale API key
- `HEADCNI_AUTH_KEY`: Alternative name for Headscale API key

### Shell Completion Setup

#### Bash
```bash
# Load completion for current session
source <(headcni completion bash)

# Load completion for all sessions
headcni completion bash > ~/.local/share/bash-completion/completions/headcni
```

#### Zsh
```bash
# Load completion for current session
source <(headcni completion zsh)

# Load completion for all sessions
headcni completion zsh > "${fpath[1]}/_headcni"
```

#### Fish
```bash
# Load completion for current session
headcni completion fish | source

# Load completion for all sessions
headcni completion fish > ~/.config/fish/completions/headcni.fish
```

## Troubleshooting

### Common Issues

1. **Cluster Connection Failed**
   ```bash
   # Check kubectl configuration
   kubectl cluster-info
   
   # Verify cluster access
   kubectl auth can-i create daemonset
   ```

2. **Installation Fails**
   ```bash
   # Check prerequisites
   headcni install --dry-run
   
   # View detailed logs
   headcni logs --follow
   ```

3. **Connectivity Issues**
   ```bash
   # Run connectivity tests
   headcni connect-test --verbose
   
   # Check Tailscale status
   tailscale status
   ```

4. **Metrics Not Available**
   ```bash
   # Check if metrics port is accessible
   headcni metrics --port 9090
   
   # Verify port forwarding
   kubectl port-forward pod/headcni-xxx 9090:9090 -n kube-system
   ```

### Getting Help

```bash
# General help
headcni --help

# Command-specific help
headcni install --help

# Version information
headcni version
```

## Development

### Building

```bash
# Build for current platform
go build -o bin/headcni cmd/cli/main.go

# Build for multiple platforms
make build-cli-cross

# Build with specific version
VERSION=v1.0.0 make build-cli
```

### Testing

```bash
# Run all tests
go test ./...

# Run specific command tests
go test ./cmd/cli/commands/...

# Run with coverage
go test -cover ./...
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## License

This project is licensed under the MIT License - see the [LICENSE](../../LICENSE) file for details. 