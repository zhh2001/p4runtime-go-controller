# API reference

The links below open the Go reference for **v2.0.1**. Import SDK packages under `github.com/zhh2001/p4runtime-go-controller/v2`.

| Package                                                                                          | Purpose                                                                          |
| ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| [client](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/client)         | Connections, arbitration, pipeline RPCs, reads, writes and stream callbacks.     |
| [pipeline](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/pipeline)     | Loading P4Info and device configuration, resource lookup and copied definitions. |
| [tableentry](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/tableentry) | Direct-action table entry and key builders.                                      |
| [codec](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/codec)           | Integer and address encoding helpers.                                            |
| [packetio](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/packetio)     | PacketIn and PacketOut with named metadata.                                      |
| [digest](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/digest)         | Named subscriptions and digest acknowledgements.                                 |
| [counter](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/counter)       | Indirect counter reads and writes.                                               |
| [meter](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/meter)           | Indirect meter reads, configuration and reset.                                   |
| [register](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/register)     | Register reads and validated P4Data writes.                                      |
| [pre](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/pre)               | Multicast groups and clone sessions.                                             |
| [errors](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/errors)         | Error sentinels and per-update write results.                                    |
| [metrics](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1/metrics)       | Reserved package for planned metrics support. It has no collector API.           |

Start with the [quickstart](quickstart.md) for a complete program. Resource guides cover [pipeline ownership](../pipeline/README.md), [counters](../counter/README.md), [meters](../meter/README.md), [registers](../register/README.md) and [packet replication](../pre/README.md).
