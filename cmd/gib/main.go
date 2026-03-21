package main

import (
	"context"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "gib",
		Short: "Go Container Builder - daemonless container image builder",
		Long:  "Build container images without Docker, compatible with jib.yaml build files.",
	}

	root.AddCommand(newBuildCmd())

	if err := fang.Execute(context.Background(), root, fang.WithNotifySignal(os.Interrupt)); err != nil {
		os.Exit(1)
	}
}
