# Quickstart

This guide walks through installing the SDK, bringing up a local BMv2 target,
and writing your first table entry — start to finish.

## 1. Install

```sh
go get github.com/zhh2001/p4runtime-go-controller@latest
```

If you only want the CLI:

```sh
go install github.com/zhh2001/p4runtime-go-controller/cmd/p4ctl@latest
```

## 2. Bring up BMv2

The repository ships a Docker wrapper that starts `simple_switch_grpc`:

```sh
./scripts/run-bmv2.sh -p 9559
```

If you prefer to run BMv2 yourself, point the examples at whatever address it
listens on.

## 3. Your first program

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/zhh2001/p4runtime-go-controller/client"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    c, err := client.Dial(ctx, "127.0.0.1:9559",
        client.WithDeviceID(1),
        client.WithElectionID(client.ElectionID{Low: 1}),
        client.WithInsecure(),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    if err := c.BecomePrimary(ctx); err != nil {
        log.Fatal(err)
    }
    log.Printf("primary for device %d", c.DeviceID())
}
```

## 4. Push a pipeline and write an entry

```go
p, err := pipeline.LoadText(p4infoBytes, deviceConfigBytes)
res, _ := c.SetPipeline(ctx, p, client.SetPipelineOptions{})
log.Printf("installed via %s", res.Action)

entry, _ := tableentry.NewBuilder(p, "MyIngress.t_l2").
    Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
    Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
    Build()
c.WriteTableEntry(ctx, client.UpdateInsert, entry)
```

## 5. Subscribe to packet-ins

```go
sub, _ := packetio.NewSubscriber(c, p)
sub.OnPacket(func(ctx context.Context, pkt *packetio.PacketIn) {
    log.Printf("packet on port %v, %d bytes",
        pkt.Metadata["ingress_port"], len(pkt.Payload))
})
```

## Where to go next

- [`ARCHITECTURE.md`](../ARCHITECTURE.md) — how the SDK is layered.
- [`examples/`](../examples) — four end-to-end programs.
- [`cmd/p4ctl/README.md`](../cmd/p4ctl/README.md) — reference CLI workflows.
- [`docs/troubleshooting.md`](./troubleshooting.md) — common issues and fixes.
- [`docs/glossary.md`](./glossary.md) — P4 domain terms, explained.
