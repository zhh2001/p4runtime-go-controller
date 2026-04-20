// Package cmd hosts the p4ctl cobra command tree.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is set by main and printed by the --version flag.
var Version = "dev"

// Global flags shared by every subcommand.
type globalFlags struct {
	Addr       string
	DeviceID   uint64
	Election   uint64
	Role       string
	Insecure   bool
	ConfigPath string
	Output     string
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
	rootCmd.PersistentFlags().StringVar(&g.ConfigPath, "config", "", "path to config file (default $HOME/.p4ctl.yaml)")
	rootCmd.PersistentFlags().StringVar(&g.Output, "output", "table", "output format: table|json|yaml")

	rootCmd.AddCommand(connectCmd, pipelineCmd, tableCmd, packetCmd, counterCmd, versionCmd)
}

func initConfig() error {
	v := viper.New()
	v.SetEnvPrefix("P4CTL")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	if g.ConfigPath != "" {
		v.SetConfigFile(g.ConfigPath)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(home)
			v.SetConfigName(".p4ctl")
		}
	}
	if err := v.ReadInConfig(); err == nil {
		// A file was found; let it override defaults we have not set
		// from a command-line flag.
		if !rootCmd.PersistentFlags().Changed("addr") && v.IsSet("addr") {
			g.Addr = v.GetString("addr")
		}
		if !rootCmd.PersistentFlags().Changed("device-id") && v.IsSet("device-id") {
			g.DeviceID = v.GetUint64("device-id")
		}
		if !rootCmd.PersistentFlags().Changed("election-id") && v.IsSet("election-id") {
			g.Election = v.GetUint64("election-id")
		}
		if !rootCmd.PersistentFlags().Changed("role") && v.IsSet("role") {
			g.Role = v.GetString("role")
		}
	}
	return nil
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print p4ctl version",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), Version)
	},
}
