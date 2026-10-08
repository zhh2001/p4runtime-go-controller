# Packet replication

`pre.Writer` reads and writes multicast groups and clone sessions through P4Runtime. Use a nonzero group or session ID. Pass ID `0` to either read method to list all entries of that type.

## Port fields

Choose one port field for each replica:

```go
legacy := pre.Replica{EgressPort: 7, Instance: 1}
modern := pre.Replica{Port: []byte{7}, Instance: 1}
```

`EgressPort` uses the legacy `uint32 egress_port` field. `Port` uses the byte field introduced in P4Runtime 1.4. A nil `Port` selects `EgressPort`, which must be nonzero. A non-nil `Port` must contain at least one byte, and `EgressPort` must be zero. Conflicting fields and empty ports return an error before Write is called.

Port bytes are opaque. The SDK preserves leading zeros, wide values, string bytes and embedded zeros without converting them to integers. The target determines which representations it accepts. `Port: []byte{0}` is distinct from a missing port and is passed to the target for validation.

Reads retain the oneof field returned by the target. A byte port populates `Port` and leaves `EgressPort` zero. A legacy port populates `EgressPort` and leaves `Port` nil. Use the populated field when inspecting a result. Writing a read result retains its original field and bytes.

There is no automatic conversion or retry with the other port field. Use `EgressPort` for a target that only supports the legacy field.

## Backup replicas

P4Runtime 1.5 adds an ordered list of backups to each replica:

```go
group := pre.MulticastGroup{
    ID: 19,
    Replicas: []pre.Replica{{
        Port: []byte{7}, Instance: 1,
        BackupReplicas: []pre.BackupReplica{
            {Port: []byte{8}, Instance: 2},
            {Port: []byte{9}, Instance: 3},
        },
    }},
}
err := writer.InsertMulticastGroup(ctx, group)
```

Each backup requires nonempty port bytes. Reads and writes retain every backup's bytes, instance and order. The target must support backup replicas and implements the failover behavior. The SDK does not monitor port status.

Primary and backup port slices are copied during encoding and decoding. A malformed response, including a missing primary port or an empty backup port, returns an error with the group or session ID and replica index. The read method returns no partial result.

## Updating callers

Existing keyed literals such as `pre.Replica{EgressPort: 7, Instance: 1}` still work. Convert positional literals to keyed literals. `Replica` now contains slices, so it cannot be compared with `==` or used as a map key. Compare port bytes with `bytes.Equal`, and compare the backup list in order.
