# Pipeline ownership

`New`, `Load` and `LoadText` create a Pipeline that owns its P4Info and device configuration. Changes to constructor inputs after the call returns do not affect the Pipeline. Keep the inputs unchanged while construction is in progress.

`Info` returns a deep copy of P4Info. `DeviceConfig` returns a copy of the device bytes. Resource queries return independent definitions, including nested match fields, action parameters, action references and packet metadata. `Raw` returns a copy of the original resource message. Editing Raw does not update the definition's fields or the Pipeline.

Names, aliases and IDs still identify the same resource, but repeated queries return different pointers. Compare resource IDs when checking identity:

```go
table, ok := p.Table("ingress.table")
if !ok {
    return fmt.Errorf("table not found")
}
byID, ok := p.TableByID(table.ID)
sameResource := ok && table.ID == byID.ID
```

Within one returned table, action or metadata definition, its field slice and field queries refer to the same copied fields. Those fields belong to that returned definition. Changing them does not alter future Pipeline queries or builders. Field edits do not rebuild the returned definition's name and ID indexes.

To use an edited P4Info, create a new Pipeline:

```go
info := p.Info()
info.Tables[0].Size = 2048
updated, err := pipeline.New(info, p.DeviceConfig())
```

The device configuration remains opaque. If a change requires a new device configuration, supply the matching compiled artifact when constructing the new Pipeline.

`New` checks the message structure before copying. Pointer cycles, typed nil oneof wrappers, missing message values in oneofs, nil message list entries and nil message map values return an error with the field path. Shared child messages are accepted when they form no cycle. Absent optional messages remain absent. Ordinary type, bit width, resource and target rules are checked by the corresponding APIs or target.

Pipeline methods can run concurrently. Each returned object can be used and edited independently. Callers must synchronize access if they share one returned object between goroutines. Queries copy the requested definition, and Info copies the full P4Info message.
