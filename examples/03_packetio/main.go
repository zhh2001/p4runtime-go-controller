// Command 03_packetio subscribes to PacketIn messages, prints a summary of
// each packet, and demonstrates sending a PacketOut at startup.
//
// The pipeline must expose "packet_in" controller metadata with an
// "ingress_port" field and "packet_out" controller metadata with an
// "egress_port" field. The bundled l2.p4info.txt in examples/testdata
// satisfies this.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func main() {
	var (
		addr       = flag.String("addr", "127.0.0.1:9559", "P4Runtime address")
		deviceID   = flag.Uint64("device-id", 1, "device ID")
		election   = flag.Uint64("election", 1, "election ID low")
		p4infoPath = flag.String("p4info", "./examples/testdata/l2.p4info.txt", "path to p4info textproto")
		sendPort   = flag.Uint("send-port", 0, "if non-zero, send a demo packet out of this egress port at startup")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	p4info, err := os.ReadFile(*p4infoPath)
	if err != nil {
		log.Fatalf("read p4info: %v", err)
	}
	p, err := pipeline.LoadText(p4info, nil)
	if err != nil {
		log.Fatalf("parse p4info: %v", err)
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	c, err := client.Dial(dialCtx, *addr,
		client.WithDeviceID(*deviceID),
		client.WithElectionID(client.ElectionID{Low: *election}),
		client.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.BecomePrimary(dialCtx); err != nil {
		log.Fatalf("arbitration: %v", err)
	}

	sub, err := packetio.NewSubscriber(c, p)
	if err != nil {
		log.Fatalf("subscriber: %v", err)
	}
	sub.OnPacket(func(_ context.Context, pkt *packetio.PacketIn) {
		log.Printf("packet-in port=%v payload=%d bytes %s",
			pkt.Metadata["ingress_port"], len(pkt.Payload), hex.EncodeToString(pkt.Payload[:min(16, len(pkt.Payload))]))
	})

	if *sendPort != 0 {
		err := sub.Send(ctx, &packetio.PacketOut{
			Payload: []byte("hello world"),
			Metadata: map[string][]byte{
				"egress_port": codec.MustEncodeUint(uint64(*sendPort), 9),
			},
		})
		if err != nil {
			log.Fatalf("packet-out: %v", err)
		}
		log.Println("sent demo packet")
	}

	<-ctx.Done()
}
