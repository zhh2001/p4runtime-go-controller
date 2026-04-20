package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Dial the target, win the arbitration, and print connection state",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		c, err := dialClient(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "connected: device_id=%d election_id=%s state=%s primary=%t\n",
			c.DeviceID(), c.ElectionID(), c.State(), c.IsPrimary())
		return nil
	},
}
