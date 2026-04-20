# Example 01: connect

Dials a P4Runtime target, wins the primary election, and prints a summary of
the installed pipeline (if any).

## Run

```sh
go run ./examples/01_connect --addr 127.0.0.1:9559 --device-id 1 --election 1
```

## What to expect

On a fresh BMv2 instance with no pipeline loaded, output looks like:

```
connected: device_id=1 election_id=0:1 state=primary
no pipeline installed — run example 02 to push one
```

Once a pipeline is installed (see example 02) the second line lists how many
tables and actions P4Info exposed.
