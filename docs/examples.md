# Examples

The four examples use the bundled L2 P4 program and Go 1.26 or newer. Run their commands from a repository checkout at `v2.0.1`. [Start BMv2](../scripts/README.md) before running them.

| Example                                             | Behavior                                                                               |
| --------------------------------------------------- | -------------------------------------------------------------------------------------- |
| [Connect](../examples/01_connect/README.md)         | Becomes primary, prints pipeline information when present and waits for Ctrl+C.        |
| [L2 forwarding](../examples/02_l2_switch/README.md) | Installs the pipeline and writes one fixed destination MAC entry forwarding to port 1. |
| [Packet I/O](../examples/03_packetio/README.md)     | Receives PacketIn and can send a frame through PacketOut at startup.                   |
| [Counters](../examples/04_counters/README.md)       | Reads the indirect counter indexed by egress port.                                     |

Run the L2 example before the Packet I/O and counter examples. It supplies a fixed entry rather than learning MAC addresses automatically.

CPU Packet I/O loopback uses port 255 and works through gRPC. Ethernet forwarding to data ports needs bound host interfaces. To obtain nonzero counter values, send traffic matching the L2 entry. The [L2 fixture guide](../examples/testdata/README.md) explains the P4 program, interfaces and packet headers.

For a complete controller that stays connected and receives packets, use the [quickstart](quickstart.md).
