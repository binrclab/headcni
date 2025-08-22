package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type CompletionOptions struct {
	Shell string
}

func NewCompletionCommand() *cobra.Command {
	opts := &CompletionOptions{}

	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for HeadCNI CLI.

This command generates completion scripts for various shells:
- bash: Bash shell completion
- zsh: Zsh shell completion  
- fish: Fish shell completion
- powershell: PowerShell completion

To load completions in your current shell session:
  # Bash
  source <(headcni completion bash)

  # Zsh
  source <(headcni completion zsh)

  # Fish
  headcni completion fish | source

To load completions for all new sessions, write to a file and source in your shell's rc file:
  # Bash
  headcni completion bash > ~/.local/share/bash-completion/completions/headcni

  # Zsh
  headcni completion zsh > "${fpath[1]}/_headcni"

  # Fish
  headcni completion fish > ~/.config/fish/completions/headcni.fish`,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Shell = args[0]
			return runCompletion(opts)
		},
	}

	return cmd
}

func runCompletion(opts *CompletionOptions) error {
	// 获取根命令
	rootCmd := getRootCommand()
	if rootCmd == nil {
		return fmt.Errorf("failed to get root command")
	}

	// 根据shell类型生成补全脚本
	switch opts.Shell {
	case "bash":
		return rootCmd.GenBashCompletion(os.Stdout)
	case "zsh":
		return rootCmd.GenZshCompletion(os.Stdout)
	case "fish":
		return rootCmd.GenFishCompletion(os.Stdout, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletion(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell: %s", opts.Shell)
	}
}

// getRootCommand 获取根命令
// 这是一个辅助函数，用于获取根命令以生成补全脚本
func getRootCommand() *cobra.Command {
	// 创建一个临时的根命令，包含所有子命令
	rootCmd := &cobra.Command{
		Use:   "headcni",
		Short: "HeadCNI - Kubernetes CNI plugin for Headscale/Tailscale",
		Long: `HeadCNI is a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality.

It provides seamless networking for Kubernetes clusters using Tailscale's secure mesh network.`,
	}

	// 添加所有子命令
	rootCmd.AddCommand(NewInstallCommand())
	rootCmd.AddCommand(NewStatusCommand())
	rootCmd.AddCommand(NewConnectTestCommand())
	rootCmd.AddCommand(NewUninstallCommand())
	rootCmd.AddCommand(NewConfigCommand())
	rootCmd.AddCommand(NewLogsCommand())
	rootCmd.AddCommand(NewMetricsCommand())
	rootCmd.AddCommand(NewUpgradeCommand())
	rootCmd.AddCommand(NewDiagnosticsCommand())
	rootCmd.AddCommand(NewCompletionCommand())

	return rootCmd
}
