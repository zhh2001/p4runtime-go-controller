# p4ctl

Reference CLI built on top of `p4runtime-go-controller`. Dogfood for the SDK and a starting point for your own controller.

## Install

```sh
go install github.com/zhh2001/p4runtime-go-controller/cmd/p4ctl@latest
```

## Global flags

Every subcommand honors these persistent flags:

```
--addr string            target address (default "127.0.0.1:9559")
--device-id uint         device ID (default 1)
--election-id uint       election ID, low 64 bits (default 1)
--role string            role name, empty = full access
--insecure               disable TLS (default true)
--tls-ca string          PEM CA bundle for TLS (default system roots)
--tls-server-name string  TLS server name to verify (default target hostname)
--tls-cert string        PEM client certificate for mutual TLS
--tls-key string         PEM client private key for mutual TLS
--config-file string     CLI settings file (default $HOME/.p4ctl.yaml)
--config string          legacy alias, except pipeline set (device config)
--output string          output format: table|json|yaml (default "table")
```

Settings use command-line flags first, then nonempty environment variables, then the configuration file, then defaults. An explicitly supplied flag, including `--insecure=false` or `--role=""`, overrides other sources. Empty environment variables are ignored.

| Setting           | Environment variable    |
| ----------------- | ----------------------- |
| `addr`            | `P4CTL_ADDR`            |
| `device-id`       | `P4CTL_DEVICE_ID`       |
| `election-id`     | `P4CTL_ELECTION_ID`     |
| `role`            | `P4CTL_ROLE`            |
| `insecure`        | `P4CTL_INSECURE`        |
| `tls-ca`          | `P4CTL_TLS_CA`          |
| `tls-server-name` | `P4CTL_TLS_SERVER_NAME` |
| `tls-cert`        | `P4CTL_TLS_CERT`        |
| `tls-key`         | `P4CTL_TLS_KEY`         |
| `output`          | `P4CTL_OUTPUT`          |
| CLI settings file | `P4CTL_CONFIG_FILE`     |

Environment settings work without a configuration file. Invalid booleans, negative or fractional IDs, IDs above `uint64`, and unsupported output formats return an error before connecting.

## TLS

Use `--insecure=false` to enable TLS with server certificate verification. Without `--tls-ca`, the client uses the system trust store. With `--tls-ca`, it trusts the certificates in that PEM bundle. TLS requires version 1.2 or later.

For a target signed by a private CA:

```sh
p4ctl connect --addr switch01.lab.internal:9559 --insecure=false \
    --tls-ca ./ca.pem
```

When connecting by IP address, `--tls-server-name` can supply the DNS name in the server certificate. That name is still verified:

```sh
p4ctl connect --addr 192.0.2.10:9559 --insecure=false \
    --tls-ca ./ca.pem --tls-server-name switch01.lab.internal
```

For mutual TLS, provide the client certificate and private key together:

```sh
p4ctl connect --addr switch01.lab.internal:9559 --insecure=false \
    --tls-ca ./ca.pem --tls-cert ./controller.pem --tls-key ./controller-key.pem
```

TLS flags require `--insecure=false`. Missing certificate pairs, unreadable files, CA bundles with no valid certificates, and invalid client certificate pairs are rejected before connecting. Certificate verification failures never cause a retry over plaintext.

## Five common workflows

### 1. Sanity-check connectivity

```sh
p4ctl connect
```

Expected output: `connected: device_id=1 election_id=0:1 state=primary primary=true`.

### 2. Push a pipeline

Compile the L2 P4Info and device config together before installing them:

```sh
./scripts/compile-l2.sh
```

```sh
p4ctl pipeline set \
    --p4info ./examples/testdata/l2.p4info.txt \
    --config ./examples/testdata/l2.bmv2.json
```

The CLI tries `VERIFY_AND_COMMIT`, then `RECONCILE_AND_COMMIT` if the first action is explicitly unsupported. It reports which action succeeded.

### 3. Insert a table entry

```sh
p4ctl table insert \
    --p4info ./examples/testdata/l2.p4info.txt \
    --table MyIngress.t_l2 \
    --match "hdr.eth.dst=00:11:22:33:44:55" \
    --action MyIngress.forward \
    --param "port=1"
```

Match syntax:

- `field=value` — EXACT
- `field=value/prefix` — LPM (prefix is bits)
- `field=value&mask` — TERNARY
- `field=low..high` — RANGE
- `field=?value` — OPTIONAL

Delete an entry using its match fields, without an action or parameters:

```sh
p4ctl table delete \
    --p4info ./examples/testdata/l2.p4info.txt \
    --table MyIngress.t_l2 \
    --match "hdr.eth.dst=00:11:22:33:44:55"
```

For tables with TERNARY, RANGE, or OPTIONAL fields, pass the same positive `--priority` used for insertion. EXACT and LPM tables require priority zero. Every EXACT field must be supplied. Wildcard fields identify a wildcard entry with that key and priority, so omitting them does not delete all matching entries.

### 4. Send a packet out a specific port

```sh
p4ctl packet send --p4info ./examples/testdata/l2.p4info.txt \
    --hex 0011223344550066778899aa88b568656c6c6f20776f726c6400000000000000000000000000000000000000000000000000000000000000000000000000 --port 1
```

The payload is a complete Ethernet frame. The L2 program uses CPU port 255 for Packet I/O, and BMv2 must bind a host-facing interface to output port 1.

The command requires `packet_out` metadata with an `egress_port` field. It uses that field's declared bit width and supplies zero for `_pad` when present. Port zero is sent as `00`; an out-of-range port returns an error. Pipelines with additional metadata fields require an SDK application that supplies all fields.

The command waits for gRPC to send the request, half-closes the stream, and waits for the target's final RPC status before exiting. Its 10-second deadline also covers connection setup. A timeout or failed final status returns an error. Successful stream completion does not acknowledge individual packet forwarding. Use [example 03](../../examples/03_packetio/README.md) for a session that continues receiving PacketIn.

### 5. Read indirect counters

```sh
p4ctl counter read --p4info ./examples/testdata/l2.p4info.txt \
    --counter MyIngress.pkt_counter
```

## Configuration file

`p4ctl` looks for `$HOME/.p4ctl.yaml` by default. A missing default file is optional. An unreadable or malformed file returns an error. Select another file with `--config-file` or `P4CTL_CONFIG_FILE`. An explicit flag overrides the environment path, and an explicitly selected file must exist.

The legacy global `--config` still selects CLI settings for commands other than `pipeline set`. Supply only one global settings flag. For `pipeline set`, `--config` always refers to the compiled device configuration, so use `--config-file` for CLI settings:

```sh
p4ctl --config-file ./controller.yaml pipeline set \
    --p4info ./examples/testdata/l2.p4info.txt \
    --config ./examples/testdata/l2.bmv2.json
```

YAML and JSON settings files accept the same keys as the global flags. Example with mutual TLS:

```yaml
addr: switch01.lab.internal:9559
device-id: 1
election-id: 1
role: ""
insecure: false
tls-ca: ./ca.pem
tls-server-name: switch01.lab.internal
tls-cert: ./controller.pem
tls-key: ./controller-key.pem
output: json
```

Device and election IDs retain their full 64-bit precision in both formats. Relative certificate paths are resolved from the current working directory.

## Output formats

The default `--output=table` keeps the human-readable text. Use `--output=json` or `--output=yaml` for these commands:

| Command        | Output                                                              |
| -------------- | ------------------------------------------------------------------- |
| `connect`      | Object with `device_id`, `election_id`, `state`, and `primary`      |
| `pipeline set` | Object with the successful `action` and ordered `attempted` actions |
| `pipeline get` | Full P4Info object                                                  |
| `table read`   | Array of TableEntry objects                                         |
| `counter read` | Array of objects with `name`, `id`, `index`, `packets`, and `bytes` |

Empty read results are `[]`. P4Info and TableEntry use the [official protobuf JSON mapping](https://protobuf.dev/programming-guides/json/), including camelCase field names, base64 bytes, and strings for 64-bit integers. YAML preserves the same values and types. Counter indices and counts, and the connection's device ID, are also strings to preserve precision. The election ID uses `high:low` notation.

```sh
p4ctl --output=json pipeline get
p4ctl --output=yaml counter read \
    --p4info ./examples/testdata/l2.p4info.txt --counter MyIngress.pkt_counter --index 1
```

Diagnostics go to stderr, leaving structured stdout ready for parsing. Packet sniffing, version, and help keep their text output. Commands that produce no result remain silent.
