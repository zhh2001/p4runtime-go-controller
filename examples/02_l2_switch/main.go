// Command 02_l2_switch pushes a pipeline and populates a small L2 MAC table.
// It assumes the target is BMv2 and the P4 program declares a table named
// "ingress.t_l2" with one EXACT match on hdr.eth.dst and an action named
// "ingress.forward(port)" taking a 9-bit egress port.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func main() {
	var (
		addr       = flag.String("addr", "127.0.0.1:9559", "P4Runtime address")
		deviceID   = flag.Uint64("device-id", 1, "device ID")
		election   = flag.Uint64("election", 1, "election ID low")
		p4infoPath = flag.String("p4info", "./examples/testdata/l2.p4info.txt", "path to p4info textproto")
		configPath = flag.String("config", "./examples/testdata/l2.bmv2.json", "path to device config (bmv2.json)")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	p, err := loadPipeline(*p4infoPath, *configPath)
	if err != nil {
		log.Fatalf("load pipeline: %v", err)
	}

	c, err := client.Dial(ctx, *addr,
		client.WithDeviceID(*deviceID),
		client.WithElectionID(client.ElectionID{Low: *election}),
		client.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.BecomePrimary(ctx); err != nil {
		log.Fatalf("arbitration: %v", err)
	}

	res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	if err != nil {
		log.Fatalf("set pipeline: %v", err)
	}
	log.Printf("pipeline installed via %v", res.Action)

	// Populate a single L2 entry: hdr.eth.dst=00:11:22:33:44:55 → port 1.
	entry, err := tableentry.NewBuilder(p, "ingress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("ingress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	if err != nil {
		log.Fatalf("build entry: %v", err)
	}
	if err := c.WriteTableEntry(ctx, client.UpdateInsert, entry); err != nil {
		log.Fatalf("insert entry: %v", err)
	}
	log.Println("wrote 1 entry")
}

func loadPipeline(p4infoPath, configPath string) (*pipeline.Pipeline, error) {
	p4info, err := os.ReadFile(p4infoPath)
	if err != nil {
		return nil, err
	}
	var cfg []byte
	if configPath != "" {
		cfg, err = os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
	}
	return pipeline.LoadText(p4info, cfg)
}
