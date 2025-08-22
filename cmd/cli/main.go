package main

import (
	"fmt"
	"os"

	"github.com/binrclab/headcni/cmd/cli/commands"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "unknown"
	date      = "unknown"
	buildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "headcni",
		Short: "HeadCNI - Kubernetes CNI plugin for Headscale/Tailscale",
		Long: `HeadCNI is a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality.

It provides seamless networking for Kubernetes clusters using Tailscale's secure mesh network.

Features:
• Zero-configuration networking with automatic Tailscale discovery
• High-performance network forwarding based on veth pairs
• Security leveraging Tailscale's WireGuard encryption
• Simple deployment without additional etcd clusters
• Built-in Prometheus metrics and monitoring
• Multi-strategy IPAM support
• MagicDNS integration

For more information, visit: https://github.com/binrclab/headcni`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildTime),
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	// 添加子命令
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewStatusCommand())
	rootCmd.AddCommand(commands.NewConnectTestCommand())
	rootCmd.AddCommand(commands.NewUninstallCommand())
	rootCmd.AddCommand(commands.NewConfigCommand())
	rootCmd.AddCommand(commands.NewLogsCommand())
	rootCmd.AddCommand(commands.NewMetricsCommand())
	rootCmd.AddCommand(commands.NewUpgradeCommand())
	rootCmd.AddCommand(commands.NewDiagnosticsCommand())
	rootCmd.AddCommand(commands.NewBackupCommand())
	rootCmd.AddCommand(commands.NewRestoreCommand())
	rootCmd.AddCommand(commands.NewCompletionCommand())

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
