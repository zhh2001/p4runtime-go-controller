# Register arrays

`register.Reader` reads and writes register arrays by P4Info name or alias. Reads return raw `P4Data` values. Writes validate the register's type before sending a `MODIFY` request.

Use `Write` for `bit<W>` and `int<W>` values:

```go
r, err := register.NewReader(c, p)
if err != nil {
    return err
}
// A bit<9> register receives 511 as 01 ff.
err = r.Write(ctx, "state", 0, []byte{0x00, 0x01, 0xff})
```

Integer bytes use big-endian order. Signed integers use two's complement, with the first byte's high bit indicating the sign. For example, `00 80` is positive 128, `80` is negative 128, and `ff 7f` is negative 129. The SDK checks the declared width and removes redundant zero or sign extension bytes. Empty integer input becomes `00`.

Use `WriteData` for other P4 types. The oneof field must match the P4Info declaration. A bool register, for example, requires an explicit bool field even for false:

```go
err = r.WriteData(ctx, "enabled", 0, &p4v1.P4Data{
    Data: &p4v1.P4Data_Bool{Bool: false},
})
```

Tuples and structs require every member in declaration order. Headers require the declared fields in order when valid, and no fields when invalid. A header union specifies its valid member by name, or leaves both the name and header unset when invalid. Header and header union stacks require exactly the declared number of entries.

Safe enums and errors use declared member names. Serializable enums use `P4Data.enum_value` and may contain any value that fits the underlying width. Named types follow their original type or their translated SDN representation. Numeric translations use the SDN width. String translations use `P4Data.bitstring` and retain all bytes, including an empty value and leading zeros.

Varbits use `P4Data.varbit` with an explicit `bitwidth` between zero and the declared maximum. The byte length must be `ceil(bitwidth / 8)`, and unused high bits must be zero. Leading zero bytes are retained because they contribute to the length. Zero bits require an empty byte string. Varbit fields in valid headers are unsupported: `P4Header` has no field for their dynamic bitwidth.

Both write methods copy the accepted value before making an RPC. Invalid types, shapes, widths and missing declarations return locally without a write request. A nil `P4Data` is an error. Numeric overflow wraps `errors.ErrInvalidBitWidth`.

Pass `-1` to `Read` for the whole array. Other reads and all writes require a non-negative index. Ordinary indexes must be below the P4Info array size. Named index types retain their numeric value for the target to translate and validate.

Target support for RegisterEntry and individual P4 types varies. Local validation does not establish target support. An unsupported write returns `errors.ErrTargetUnsupported` when the target reports `UNIMPLEMENTED`.
