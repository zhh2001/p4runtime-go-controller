package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/zhh2001/p4runtime-go-controller/counter"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

var (
	counterP4Info string
	counterName   string
	counterIndex  int64
)

var counterCmd = &cobra.Command{Use: "counter", Short: "Read or write P4 counters"}

var counterReadCmd = &cobra.Command{
	Use:   "read",
	Short: "Read one or all indexes of an indirect counter",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if counterP4Info == "" || counterName == "" {
			return fmt.Errorf("--p4info and --counter required")
		}
		raw, err := os.ReadFile(counterP4Info)
		if err != nil {
			return err
		}
		p, err := pipeline.LoadText(raw, nil)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		c, err := dialClient(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		r, err := counter.NewReader(c, p)
		if err != nil {
			return err
		}
		entries, err := r.Read(ctx, counterName, counterIndex)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Fprintf(cmd.OutOrStdout(), "%s[%d]: packets=%d bytes=%d\n",
				e.Name, e.Index, e.Packets, e.Bytes)
		}
		return nil
	},
}

func init() {
	counterReadCmd.Flags().StringVar(&counterP4Info, "p4info", "", "path to P4Info text proto (required)")
	counterReadCmd.Flags().StringVar(&counterName, "counter", "", "counter name (required)")
	counterReadCmd.Flags().Int64Var(&counterIndex, "index", -1, "counter index (-1 = all)")
	counterCmd.AddCommand(counterReadCmd)
}
