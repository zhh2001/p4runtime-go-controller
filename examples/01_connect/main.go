// Command 01_connect dials a P4Runtime target, elects itself primary, and
// prints a summary of the installed pipeline.
//
// Run against BMv2 (or any other P4Runtime target) like so:
//
//	go run ./examples/01_connect --addr 127.0.0.1:9559 --device-id 1 --election 1
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/client"
	perrors "github.com/zhh2001/p4runtime-go-controller/errors"
)

func main() {
	var (
		addr     = flag.String("addr", "127.0.0.1:9559", "P4Runtime target address")
		deviceID = flag.Uint64("device-id", 1, "target device ID")
		election = flag.Uint64("election", 1, "election ID (low 64 bits)")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
	fmt.Printf("connected: device_id=%d election_id=%s state=%s\n",
		c.DeviceID(), c.ElectionID(), c.State())

	pipeline, err := c.GetPipeline(ctx)
	switch {
	case err == nil:
		fmt.Printf("pipeline installed: %d tables, %d actions\n",
			len(pipeline.Tables()), len(pipeline.Info().GetActions()))
	case errorsIs(err, perrors.ErrPipelineNotSet):
		fmt.Println("no pipeline installed — run example 02 to push one")
	default:
		log.Fatalf("get pipeline: %v", err)
	}

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "shutting down")
}

// errorsIs is a tiny local helper to avoid importing stdlib `errors` twice.
func errorsIs(err, target error) bool {
	type iser interface{ Is(error) bool }
	for err != nil {
		if err == target {
			return true
		}
		if ix, ok := err.(iser); ok && ix.Is(target) {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		break
	}
	return false
}
