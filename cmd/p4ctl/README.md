# p4ctl

Reference CLI built on top of `p4runtime-go-controller`. Dogfood for the SDK
and a starting point for your own controller.

## Install

```sh
go install github.com/zhh2001/p4runtime-go-controller/cmd/p4ctl@latest
```

## Global flags

Every subcommand honors these persistent flags:

```
--addr string         target address (default "127.0.0.1:9559")
--device-id uint      device ID (default 1)
--election-id uint    election ID, low 64 bits (default 1)
--role string         role name, empty = full access
--insecure            disable TLS (default true; early dev uses plaintext)
--config string       path to config file (default $HOME/.p4ctl.yaml)
--output string       output format: table|json|yaml (default "table")
```

Env-var overrides: `P4CTL_ADDR`, `P4CTL_DEVICE_ID`, `P4CTL_ELECTION_ID`,
`P4CTL_ROLE`.

## Five common workflows

### 1. Sanity-check connectivity

```sh
p4ctl connect
```

Expected output: `connected: device_id=1 election_id=0:1 state=primary primary=true`.

### 2. Push a pipeline

```sh
p4ctl pipeline set \
    --p4info ./examples/testdata/l2.p4info.txt \
    --config ./examples/testdata/l2.bmv2.json
```

The CLI walks the fallback chain (`VERIFY_AND_COMMIT` → `RECONCILE_AND_COMMIT`
→ `COMMIT`) automatically and reports which action succeeded.

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

### 4. Send a packet out a specific port

```sh
p4ctl packet send --p4info ./examples/testdata/l2.p4info.txt \
    --hex deadbeef --port 1
```

### 5. Read indirect counters

```sh
p4ctl counter read --p4info ./examples/testdata/l2.p4info.txt \
    --counter MyIngress.pkt_counter
```

## Configuration file

`p4ctl` looks for `$HOME/.p4ctl.yaml` by default. Example:

```yaml
addr: switch01.lab.internal:9559
device-id: 1
election-id: 1
role: ""
```

Values in the file are used only when the matching flag has not been set on
the command line.
