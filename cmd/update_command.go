package cmd

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	. "github.com/leptonai/xray-manager/pkg/logger"
	"github.com/leptonai/xray-manager/pkg/ssh"
	"github.com/leptonai/xray-manager/pkg/xray"
)

var (
	arch      string
	sshClient ssh.SSH
	domain    string
)

func NewUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "update",
		Short:      "Implements update xray sub-commands",
		Aliases:    []string{"update", "u"},
		SuggestFor: []string{"update", "u"},
	}

	cmd.AddCommand(
		newUpdateClientCommand(),
	)

	cmd.PersistentFlags().StringVar(&arch, "arch", "amd64", "target machine arch, supports amd64 and arm64-v8a")
	cmd.PersistentFlags().StringVar(&sshClient.Host, "host", "", "target machine ip/hostname")
	cmd.PersistentFlags().StringVar(&sshClient.PrivateKeyFile, "private-key", "", "path to private key")
	cmd.PersistentFlags().StringVar(&sshClient.User, "user", "", "target machine user")
	cmd.PersistentFlags().IntVar(&sshClient.Port, "port", 22, "target machine port")
	cmd.PersistentFlags().StringVar(&domain, "domain", domain, "r2 domain stores xray installer package")
	return cmd
}

func newUpdateClientCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "update xray client",
		Run:   updateClientFunc,
	}
	return cmd
}

func updateClientFunc(cmd *cobra.Command, args []string) {
	if sshClient.Host != "" {
		updateClient(&sshClient)
		return
	}
}

func updateClient(sshClient *ssh.SSH) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	installer := xray.NewInstaller(sshClient, os.Stderr)
	err := installer.UpdateXRayClient(ctx, arch, domain)
	if err != nil {
		Logger.Fatalf("faield to update xray, err: %v", err)
	}
}
