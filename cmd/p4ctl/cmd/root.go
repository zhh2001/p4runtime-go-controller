// Package cmd hosts the p4ctl cobra command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set by main and printed by the version command.
var Version = "dev"

// Global flags shared by every subcommand.
type globalFlags struct {
	Addr          string
	DeviceID      uint64
	Election      uint64
	Role          string
	Insecure      bool
	TLSCA         string
	TLSServerName string
	TLSCert       string
	TLSKey        string
	ConfigPath    string
	ConfigFile    string
	Output        string
}

var g globalFlags

// rootCmd is the top-level cobra command.
var rootCmd = &cobra.Command{
	Use:          "p4ctl",
	Short:        "Reference CLI for p4runtime-go-controller",
	SilenceUsage: true,
}

// Execute runs the root command and is the entry point from main.
func Execute() error { return rootCmd.Execute() }

func init() {
	rootCmd.PersistentPreRunE = func(*cobra.Command, []string) error { return initConfig() }
	rootCmd.PersistentFlags().StringVar(&g.Addr, "addr", "127.0.0.1:9559", "P4Runtime target address")
	rootCmd.PersistentFlags().Uint64Var(&g.DeviceID, "device-id", 1, "target device ID")
	rootCmd.PersistentFlags().Uint64Var(&g.Election, "election-id", 1, "election ID (low 64 bits)")
	rootCmd.PersistentFlags().StringVar(&g.Role, "role", "", "role name (empty for full access)")
	rootCmd.PersistentFlags().BoolVar(&g.Insecure, "insecure", true, "disable TLS (default true)")
	rootCmd.PersistentFlags().StringVar(&g.TLSCA, "tls-ca", "", "PEM CA bundle for TLS (default system roots)")
	rootCmd.PersistentFlags().StringVar(&g.TLSServerName, "tls-server-name", "", "TLS server name to verify (default target hostname)")
	rootCmd.PersistentFlags().StringVar(&g.TLSCert, "tls-cert", "", "PEM client certificate for mutual TLS")
	rootCmd.PersistentFlags().StringVar(&g.TLSKey, "tls-key", "", "PEM client private key for mutual TLS")
	rootCmd.PersistentFlags().StringVar(&g.ConfigFile, "config-file", "", "path to CLI config file (default $HOME/.p4ctl.yaml)")
	rootCmd.PersistentFlags().StringVar(&g.ConfigPath, "config", "", "alias for --config-file (except pipeline set)")
	rootCmd.PersistentFlags().StringVar(&g.Output, "output", "table", "output format: table|json|yaml")

	rootCmd.AddCommand(connectCmd, pipelineCmd, tableCmd, packetCmd, counterCmd, versionCmd)
}

func initConfig() error {
	home, _ := os.UserHomeDir()
	flags, err := loadGlobalFlags(rootCmd.PersistentFlags(), home)
	if err != nil {
		return err
	}
	g = flags
	return nil
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print p4ctl version",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), Version)
	},
}
