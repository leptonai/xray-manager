package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"
	xrayconfig "github.com/xtls/xray-core/infra/conf"

	. "github.com/leptonai/xray-manager/pkg/logger"
	"github.com/leptonai/xray-manager/pkg/s3/r2"
	"github.com/leptonai/xray-manager/pkg/xray"
)

var (
	serverName        string
	publicKey         string
	serverConfigFile  string
	weight            int
	vlessPath         string
	r2AccountID       string
	r2AccessKeyID     string
	r2AccessKeySecret string
	r2Region          string
)

const (
	defaultTimeout = 10 * time.Second
)

func NewServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "server",
		Short:      "Implements xray server sub-commands",
		Aliases:    []string{"server", "s"},
		SuggestFor: []string{"server", "s"},
	}

	cmd.AddCommand(
		newServerAddCommand(),
		newServerDeleteCommand(),
		newServerGetCommand(),
		newServerListCommand(),
	)

	cmd.PersistentFlags().StringVarP(&serverName, "server-name", "s", "", "Name of the server")
	cmd.PersistentFlags().StringVarP(&publicKey, "public-key", "p", "", "Value of the public key (required for reality security)")
	cmd.PersistentFlags().IntVarP(&weight, "weight", "w", 0, "Weight of the server, typically use the network transfer volume size of this server in TB")
	cmd.PersistentFlags().StringVarP(&vlessPath, "vless-path", "v", "", "Path of the vless server")
	cmd.PersistentFlags().StringVarP(&r2AccountID, "r2-account-id", "", "", "Account ID of the R2 object store")
	cmd.PersistentFlags().StringVarP(&r2AccessKeyID, "r2-access-key-id", "", "", "Access key ID of the R2 object store")
	cmd.PersistentFlags().StringVarP(&r2AccessKeySecret, "r2-access-key-secret", "", "", "Access key secret of the R2 object store")
	cmd.PersistentFlags().StringVarP(&r2Region, "r2-region", "", r2.RegionUS, "Region of the R2 object store")
	return cmd

}

func newServerAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "add a new server",
		Run:   addFunc,
	}

	cmd.PersistentFlags().StringVarP(&serverConfigFile, "config", "c", "", "Path to the server config file")

	return cmd
}

func newServerDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "delete an existing server",
		Run:   deleteFunc,
	}
	return cmd
}

func newServerGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "get an existing server",
		Run:   getFunc,
	}
	return cmd
}

func newServerListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "list existing servers",
		Run:   listFunc,
	}
	return cmd
}

func addFunc(cmd *cobra.Command, args []string) {
	scf, err := os.ReadFile(serverConfigFile)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	sc := xrayconfig.Config{}
	err = json.Unmarshal(scf, &sc)
	if err != nil {
		log.Fatalf("Error unmarshalling config file: %v", err)
	}

	if weight == 0 {
		log.Fatal("must set server weight")
	}
	oc, err := xray.ServerConfigToClientOutboundDetourConfig(serverName, publicKey, sc, weight, vlessPath)
	if err != nil {
		Logger.Fatal("failed to convert server config to client config: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	objectStoreCfg := *r2.NewCfg(r2AccountID, r2AccessKeyID, r2AccessKeySecret, r2.WithRegion(r2Region))
	err = xray.SaveOutboundDetourConfig(ctx, serverName, *oc, objectStoreCfg)
	if err != nil {
		log.Fatalf("Error saving config file: %v", err)
	}

	log.Println("Added a server", serverName)
}

func deleteFunc(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	objectStoreCfg := *r2.NewCfg(r2AccountID, r2AccessKeyID, r2AccessKeySecret, r2.WithRegion(r2Region))
	err := xray.DeleteOutboundDetourConfig(ctx, serverName, objectStoreCfg)
	if err != nil {
		log.Fatalf("Error removing config file: %v", err)
	}

	log.Println("Removed a server", serverName)
}

func listFunc(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	objectStoreCfg := *r2.NewCfg(r2AccountID, r2AccessKeyID, r2AccessKeySecret, r2.WithRegion(r2Region))
	ss, err := xray.ListServerNames(ctx, objectStoreCfg)
	if err != nil {
		log.Fatalf("Error removing config file: %v", err)
	}

	for _, s := range ss {
		fmt.Println(s)
	}
}

func getFunc(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	objectStoreCfg := *r2.NewCfg(r2AccountID, r2AccessKeyID, r2AccessKeySecret, r2.WithRegion(r2Region))
	oc, err := xray.GetOutboundDetourConfig(ctx, serverName, objectStoreCfg)
	if err != nil {
		log.Fatalf("Error getting config file: %v", err)
	}

	fmt.Println(oc)
}
