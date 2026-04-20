package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

var (
	packetP4Info string
	packetPort   uint64
	packetHex    string
)

var packetCmd = &cobra.Command{Use: "packet", Short: "Send or sniff PacketIn / PacketOut"}

var packetSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a PacketOut with a fixed egress_port metadata field",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if packetP4Info == "" || packetHex == "" {
			return fmt.Errorf("--p4info and --hex required")
		}
		raw, err := hex.DecodeString(packetHex)
		if err != nil {
			return fmt.Errorf("parse hex: %w", err)
		}
		info, err := os.ReadFile(packetP4Info)
		if err != nil {
			return err
		}
		p, err := pipeline.LoadText(info, nil)
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

		sub, err := packetio.NewSubscriber(c, p)
		if err != nil {
			return err
		}
		out := &packetio.PacketOut{Payload: raw}
		if packetPort > 0 {
			out.Metadata = map[string][]byte{
				"egress_port": codec.MustEncodeUint(packetPort, 9),
			}
		}
		return sub.Send(ctx, out)
	},
}

var packetSniffCmd = &cobra.Command{
	Use:   "sniff",
	Short: "Print a summary of every PacketIn that arrives",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if packetP4Info == "" {
			return fmt.Errorf("--p4info required")
		}
		info, err := os.ReadFile(packetP4Info)
		if err != nil {
			return err
		}
		p, err := pipeline.LoadText(info, nil)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
		defer dialCancel()
		c, err := dialClient(dialCtx)
		if err != nil {
			return err
		}
		defer c.Close()

		sub, err := packetio.NewSubscriber(c, p)
		if err != nil {
			return err
		}
		sub.OnPacket(func(_ context.Context, pkt *packetio.PacketIn) {
			fmt.Fprintf(cmd.OutOrStdout(), "packet-in: port=%v bytes=%d payload=%s\n",
				pkt.Metadata["ingress_port"], len(pkt.Payload),
				hex.EncodeToString(pkt.Payload[:min(16, len(pkt.Payload))]))
		})
		<-ctx.Done()
		return nil
	},
}

func init() {
	for _, sub := range []*cobra.Command{packetSendCmd, packetSniffCmd} {
		sub.Flags().StringVar(&packetP4Info, "p4info", "", "path to P4Info text proto (required)")
	}
	packetSendCmd.Flags().Uint64Var(&packetPort, "port", 0, "egress_port metadata to set on the PacketOut")
	packetSendCmd.Flags().StringVar(&packetHex, "hex", "", "raw payload in hexadecimal (required)")
	packetCmd.AddCommand(packetSendCmd, packetSniffCmd)
}
