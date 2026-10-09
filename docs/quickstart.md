# Quickstart

This guide covers installing the SDK, starting a local BMv2 target and writing your first table entry.

For an existing v1 application, start with the [v2 migration guide](migration-v2.md).

## 1. Install

Use Go 1.26 or newer. This guide uses the v2 SDK. Before the first v2 release, use a checkout whose `go.mod` declares `github.com/zhh2001/p4runtime-go-controller/v2`.

From your application's Go module, point Go at that checkout. If you are starting a new project, create its directory and run `go mod init example.com/p4-controller` there first. Replace the path below with the absolute path to your SDK checkout:

```sh
go mod edit -replace=github.com/zhh2001/p4runtime-go-controller/v2=/path/to/p4runtime-go-controller
go get github.com/zhh2001/p4runtime-go-controller/v2/client
```

For the CLI, run this from the SDK checkout:

```sh
go install ./cmd/p4ctl
```

Go installs `p4ctl` in `GOBIN`, or in `GOPATH/bin` when `GOBIN` is unset. Add that directory to `PATH` to run the CLI.

After a v2 release is published, install it without a local replacement:

```sh
go get github.com/zhh2001/p4runtime-go-controller/v2@latest
go install github.com/zhh2001/p4runtime-go-controller/v2/cmd/p4ctl@latest
```

If your application has the development replacement, remove it with `go mod edit -dropreplace=github.com/zhh2001/p4runtime-go-controller/v2` before fetching a published version. The v1 module path continues to select v1 releases.

## 2. Bring up BMv2

The bundled scripts require a repository checkout and Bash. Native mode needs `p4c-bm2-ss` and `simple_switch_grpc` on `PATH`. See [script requirements](../scripts/README.md#requirements) for Docker and integration-test tools.

Use the same SDK checkout as in the install step. To get a checkout, run:

```sh
git clone https://github.com/zhh2001/p4runtime-go-controller.git
cd p4runtime-go-controller
```

Start the local BMv2 target. The script compiles a matching L2 P4Info and device config, then uses device ID 1 and CPU port 255:

```sh
./scripts/run-bmv2.sh
```

Keep the target running in another terminal. This command supports control plane operations. To send and receive traffic, bind host-facing interfaces as described in the [L2 fixture guide](../examples/testdata/README.md).

Use `./scripts/run-bmv2.sh --docker` for a detached Docker target. Docker mode still requires local p4c. See [the script guide](../scripts/README.md) for custom ports and container names.

With the target running, run `make e2e` from the repository root in another terminal. It compiles its own matching L2 pair and runs the integration tests with race detection, which needs CGO and a supported C compiler. L2 pipeline, arbitration and CPU Packet I/O tests run automatically. Other suites require the optional settings described in [the integration guide](../test/integration/README.md).

## 3. Write the controller

Save the following as `main.go` in your application's Go module. It loads the two L2 files, becomes primary, installs the pipeline, writes a forwarding entry and keeps receiving PacketIn until Ctrl+C or SIGTERM.

Setup has a 20-second deadline. The receive loop uses the signal context and stays active after setup completes. `run` returns errors so its deferred cleanup runs before `main` exits. The subscription is canceled before closing the stream, and shutdown has its own five-second deadline.

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/codec"
	"github.com/zhh2001/p4runtime-go-controller/v2/packetio"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() (err error) {
	addr := flag.String("addr", "127.0.0.1:9559", "P4Runtime address")
	infoPath := flag.String("p4info", "", "path to L2 P4Info textproto")
	configPath := flag.String("config", "", "path to matching L2 BMv2 JSON")
	election := flag.Uint64("election", 1, "election ID low word")
	loopback := flag.Bool("loopback", false, "send a demo frame through CPU port 255")
	flag.Parse()
	if *infoPath == "" || *configPath == "" {
		return errors.New("--p4info and --config are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	info, err := os.ReadFile(*infoPath)
	if err != nil {
		return fmt.Errorf("read P4Info: %w", err)
	}
	config, err := os.ReadFile(*configPath)
	if err != nil {
		return fmt.Errorf("read device config: %w", err)
	}
	p, err := pipeline.LoadText(info, config)
	if err != nil {
		return fmt.Errorf("load pipeline: %w", err)
	}

	setupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := client.Dial(setupCtx, *addr,
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: *election}),
		client.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		err = errors.Join(err, c.CloseGracefully(closeCtx))
	}()
	if err := c.BecomePrimary(setupCtx); err != nil {
		return fmt.Errorf("arbitration: %w", err)
	}

	sub, err := packetio.NewSubscriber(c, p)
	if err != nil {
		return fmt.Errorf("subscriber: %w", err)
	}
	unsubscribe := sub.OnPacket(func(_ context.Context, pkt *packetio.PacketIn) {
		log.Printf("packet on port %v, %d bytes",
			pkt.Metadata["ingress_port"], len(pkt.Payload))
	})
	defer unsubscribe()

	res, err := c.SetPipeline(setupCtx, p, client.SetPipelineOptions{})
	if err != nil {
		return fmt.Errorf("set pipeline: %w", err)
	}
	log.Printf("pipeline installed via %s", res.Action)

	entry, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	if err != nil {
		return fmt.Errorf("build entry: %w", err)
	}
	if err := c.WriteTableEntry(setupCtx, client.UpdateInsert, entry); err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}
	log.Println("wrote 1 entry")

	if *loopback {
		frame := make([]byte, 60)
		copy(frame, []byte{
			0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
			0x00, 0x66, 0x77, 0x88, 0x99, 0xaa, 0x88, 0xb5,
		})
		if err := sub.Send(setupCtx, &packetio.PacketOut{
			Payload: frame,
			Metadata: map[string][]byte{
				"egress_port": codec.MustEncodeUint(255, 9),
				"_pad":        {0},
			},
		}); err != nil {
			return fmt.Errorf("send demo packet: %w", err)
		}
	}
	cancel()
	log.Println("listening for PacketIn, press Ctrl+C to stop")
	<-ctx.Done()
	return nil
}
```

## 4. Run the controller

From the application directory, use the matching files generated in step 2. Replace the absolute paths below with your checkout's paths:

```sh
go build -o controller .
./controller \
  --p4info /absolute/path/to/p4runtime-go-controller/examples/testdata/l2.p4info.txt \
  --config /absolute/path/to/p4runtime-go-controller/examples/testdata/l2.bmv2.json \
  --loopback
```

Use `--addr` for a different gRPC address and `--election` to set the low word of the election ID. The program uses device ID 1. Each run installs the pipeline and writes the entry for destination `00:11:22:33:44:55`, forwarding to data port 1.

## 5. Check PacketIn and exit

`--loopback` sends one 60-byte Ethernet frame through CPU port 255. The bundled program returns it as a PacketIn over gRPC, so this check works with the default native target and Docker mode. Look for:

```text
packet on port [255], 60 bytes
```

The port is shown as its canonical byte slice. Packet and setup logs can arrive in either order. Omit `--loopback` to only listen for incoming frames.

With data interfaces bound, send a frame whose destination is absent from `MyIngress.t_l2` to receive a PacketIn with the original ingress port and Ethernet payload. Matching frames are forwarded to port 1. Press Ctrl+C to cancel the subscription and close the client. The program waits for the stream's final status within the shutdown deadline and reports any error. See [stream shutdown](troubleshooting.md#ending-a-session-after-packetout) for targets that keep the response stream open.

## Where to go next

- [`ARCHITECTURE.md`](../ARCHITECTURE.md) — how the SDK is layered.
- [`examples/`](../examples) — four end-to-end programs.
- [`cmd/p4ctl/README.md`](../cmd/p4ctl/README.md) — reference CLI workflows.
- [`docs/troubleshooting.md`](./troubleshooting.md) — common issues and fixes.
- [`docs/glossary.md`](./glossary.md) — P4 domain terms, explained.
