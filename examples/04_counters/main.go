// Command 04_counters reads the current value of every index of a named
// indirect counter and prints one line per entry.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/counter"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func main() {
	var (
		addr        = flag.String("addr", "127.0.0.1:9559", "P4Runtime address")
		deviceID    = flag.Uint64("device-id", 1, "device ID")
		election    = flag.Uint64("election", 1, "election ID low")
		p4infoPath  = flag.String("p4info", "./examples/testdata/l2.p4info.txt", "path to p4info textproto")
		counterName = flag.String("counter", "ingress.pkt_counter", "counter name")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p4info, err := os.ReadFile(*p4infoPath)
	if err != nil {
		log.Fatalf("read p4info: %v", err)
	}
	p, err := pipeline.LoadText(p4info, nil)
	if err != nil {
		log.Fatalf("parse p4info: %v", err)
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

	r, err := counter.NewReader(c, p)
	if err != nil {
		log.Fatalf("reader: %v", err)
	}
	entries, err := r.Read(ctx, *counterName, -1)
	if err != nil {
		log.Fatalf("read counter: %v", err)
	}
	for _, e := range entries {
		fmt.Printf("%s[%d]: packets=%d bytes=%d\n", e.Name, e.Index, e.Packets, e.Bytes)
	}
}
