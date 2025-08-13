package main

import (
	"fmt"
	"os"

	"github.com/binrclab/headcni/cmd/cli/commands"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "headcni",
		Short: "HeadCNI - Kubernetes CNI plugin for Headscale/Tailscale",
		Long: `HeadCNI is a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality.

It provides seamless networking for Kubernetes clusters using Tailscale's secure mesh network.`,
		Version: fmt.Sprintf("%s (commit: %s, date: %s)", version, commit, date),
	}

	// 添加子命令
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewStatusCommand())
	rootCmd.AddCommand(commands.NewConnectTestCommand())
	rootCmd.AddCommand(commands.NewUninstallCommand())
	rootCmd.AddCommand(commands.NewConfigCommand())

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
