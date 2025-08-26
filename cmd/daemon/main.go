//go:build !windows
// +build !windows

package main

import (
	"github.com/binrclab/headcni/cmd/daemon/command"
	"k8s.io/klog/v2"
)

// main is the entry point for the HeadCNI daemon
func main() {
	// Create and execute the main command
	cmd := command.NewHeadCNIDaemonCommand()
	if err := cmd.Execute(); err != nil {
		klog.Fatalf("Failed to execute command: %v", err)
	}
}
