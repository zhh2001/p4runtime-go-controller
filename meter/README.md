# Indirect meters

`meter.Reader` reads, configures and resets indexed meter arrays by P4Info name or alias. Rates use the declared unit, bytes or packets per second. Burst sizes use bytes or packets. Direct meters attached to table entries require the raw client API.

```go
r, err := meter.NewReader(c, p)
if err != nil {
    return err
}
err = r.Write(ctx, "rate", 0, meter.Config{
    CIR: 1000, CBurst: 2000,
    PIR: 1500, PBurst: 3000,
})
```

`Write` validates the configuration against the meter's P4Info specification before sending a `MODIFY` request. Every rate and burst must be non-negative. Zero values are allowed. The meter type determines the remaining constraints:

| P4Info type               | Rates and bursts                                 | EBurst                             |
| ------------------------- | ------------------------------------------------ | ---------------------------------- |
| `TWO_RATE_THREE_COLOR`    | `PIR >= CIR`. CBurst and PBurst are independent. | Must be zero.                      |
| `SINGLE_RATE_THREE_COLOR` | `CIR = PIR` and `CBurst = PBurst`.               | Excess burst size, including zero. |
| `SINGLE_RATE_TWO_COLOR`   | `CIR = PIR` and `CBurst = PBurst`.               | Must be zero.                      |

Single-rate callers must supply the matching peak fields explicitly. The SDK reports mismatches rather than filling them in. For example, a single-rate three-color configuration is `meter.Config{CIR: 1000, PIR: 1000, CBurst: 2000, PBurst: 2000, EBurst: 3000}`. Use keyed struct literals when constructing a Config.

An omitted P4Info type uses the protocol's default, `TWO_RATE_THREE_COLOR`. Missing specifications, reserved or unknown units, unknown types and invalid values return locally without a Write RPC. Non-negative int64 values are sent unchanged. The target checks its own rate granularity, limits and support for single-rate modes. The SDK does not apply BMv2's uint32 burst limit to other targets.

`Write(ctx, name, index, meter.Config{})` sends an explicit zero-rate, zero-burst configuration. It does not restore the default meter behavior. Use `Reset(ctx, name, index)` to omit Config and restore GREEN for every packet. Reset leaves any per-color counters untouched. Legacy PI configurations using `-1` for the default behavior must migrate to Reset.

`Read` returns raw `MeterEntry` messages, preserving Config presence, EBurst and per-color counter data supplied by the target. Pass `-1` to read the whole array. Other reads, Write and Reset require a non-negative index below the P4Info array size. Named index types keep their numeric value for the target to translate and validate. Reset applies to one index.

The type constraints and default behavior follow [P4Runtime meter configuration](https://p4lang.github.io/p4runtime/spec/v1.5.0/P4Runtime-Spec.html#_meterentry_directmeterentry). Zero-value acceptance is an SDK policy. Configurations accepted locally can still be rejected by the target.
