package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

var (
	pipelineP4Info string
	pipelineConfig string
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Inspect or install the forwarding pipeline",
}

var pipelineSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Push P4Info + device config to the target",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if pipelineP4Info == "" {
			return fmt.Errorf("--p4info is required")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		infoBytes, err := os.ReadFile(pipelineP4Info)
		if err != nil {
			return fmt.Errorf("read p4info: %w", err)
		}
		var cfgBytes []byte
		if pipelineConfig != "" {
			cfgBytes, err = os.ReadFile(pipelineConfig)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}
		}
		p, err := pipeline.LoadText(infoBytes, cfgBytes)
		if err != nil {
			return fmt.Errorf("parse p4info: %w", err)
		}

		c, err := dialClient(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "pipeline installed via %s (attempted %v)\n", res.Action, res.Attempted)
		return nil
	},
}

var pipelineGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Fetch the installed pipeline metadata",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		c, err := dialClient(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		p, err := c.GetPipeline(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "tables=%d actions=%d counters=%d meters=%d registers=%d digests=%d\n",
			len(p.Info().GetTables()),
			len(p.Info().GetActions()),
			len(p.Info().GetCounters()),
			len(p.Info().GetMeters()),
			len(p.Info().GetRegisters()),
			len(p.Info().GetDigests()),
		)
		return nil
	},
}

func init() {
	pipelineSetCmd.Flags().StringVar(&pipelineP4Info, "p4info", "", "path to P4Info text proto (required)")
	pipelineSetCmd.Flags().StringVar(&pipelineConfig, "config", "", "path to device config blob (bmv2.json or platform binary)")
	pipelineCmd.AddCommand(pipelineSetCmd, pipelineGetCmd)
}
