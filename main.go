package main

import (
	"github.com/spf13/cobra"

	"github.com/leptonai/xray-manager/cmd"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "xray-manager",
		Short: "manage xray deployment",
	}
	rootCmd.AddCommand(
		cmd.NewServerCommand(),
		cmd.NewClientSyncCommand(),
		cmd.NewUpdateCommand(),
	)
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
