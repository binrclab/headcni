package command

import (
	"github.com/spf13/cobra"
)

// 版本信息变量，通过构建时注入
var (
	Version   = "dev"
	BuildDate = "unknown"
	GitCommit = "unknown"
)

func init() {
	rootCmd.AddCommand(newVersionCommand())
}

// newVersionCommand creates the version command
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Display the version information for HeadCNI daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("HeadCNI Daemon Version: %s\n", Version)
			cmd.Printf("Build Date: %s\n", BuildDate)
			cmd.Printf("Git Commit: %s\n", GitCommit)
		},
	}
}
