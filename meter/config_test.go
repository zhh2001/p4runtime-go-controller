package meter_test

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/v2/meter"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func configClient(t *testing.T, h *testutil.ServerHarness) (context.Context, *client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return ctx, c
}

func configReader(t *testing.T, c *client.Client, spec *p4configv1.MeterSpec, indexType string) *meter.Reader {
	t.Helper()
	var named *p4configv1.P4NamedType
	if indexType != "" {
		named = &p4configv1.P4NamedType{Name: indexType}
	}
	p, err := pipeline.New(&p4configv1.P4Info{Meters: []*p4configv1.Meter{{Preamble: &p4configv1.Preamble{Id: 0x14000001, Name: "ingress.rate", Alias: "rate"}, Size: 4, Spec: spec, IndexTypeName: named}}}, nil)
	require.NoError(t, err)
	r, err := meter.NewReader(c, p)
	require.NoError(t, err)
	return r
}

func TestMeterConfig_Valid(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := configClient(t, h)
	for _, tc := range []struct {
		name   string
		kind   p4configv1.MeterSpec_Type
		config meter.Config
	}{
		{"two rates", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 30, PBurst: 40}},
		{"equal rates", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 10, PBurst: 20}},
		{"independent bursts", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 40, PIR: 30, PBurst: 20}},
		{"zero rate with tokens", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CBurst: 1, PBurst: 2}},
		{"zero peak bucket", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 30}},
		{"two rate zero", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{}},
		{"single three color", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 10, PBurst: 20, EBurst: 30}},
		{"excess only", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{EBurst: 30}},
		{"no excess", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 10, PBurst: 20}},
		{"single three zero", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{}},
		{"single two color", p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR, meter.Config{CIR: 10, CBurst: 20, PIR: 10, PBurst: 20}},
		{"single two zero", p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR, meter.Config{}},
		{"wide two rate", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: math.MaxInt64 - 1, CBurst: math.MaxInt64, PIR: math.MaxInt64, PBurst: int64(math.MaxUint32) + 1}},
		{"wide single rate", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{CIR: math.MaxInt64, CBurst: math.MaxInt64, PIR: math.MaxInt64, PBurst: math.MaxInt64, EBurst: math.MaxInt64}},
	} {
		for _, unit := range []p4configv1.MeterSpec_Unit{p4configv1.MeterSpec_BYTES, p4configv1.MeterSpec_PACKETS} {
			t.Run(tc.name+"/"+unit.String(), func(t *testing.T) {
				r := configReader(t, c, &p4configv1.MeterSpec{Unit: unit, Type: tc.kind}, "")
				require.NoError(t, r.Write(ctx, "rate", 0, tc.config))
				h.Mu.Lock()
				req := h.WriteRequests[len(h.WriteRequests)-1]
				h.Mu.Unlock()
				require.Len(t, req.Updates, 1)
				require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
				entry := req.Updates[0].Entity.GetMeterEntry()
				require.EqualValues(t, 0x14000001, entry.MeterId)
				require.NotNil(t, entry.Index)
				require.Zero(t, entry.Index.Index)
				want := &p4v1.MeterConfig{Cir: tc.config.CIR, Cburst: tc.config.CBurst, Pir: tc.config.PIR, Pburst: tc.config.PBurst, Eburst: tc.config.EBurst}
				require.NotNil(t, entry.Config, "explicit zero must preserve Config presence")
				require.True(t, proto.Equal(want, entry.Config), "config on wire: %s", entry.Config)
			})
		}
	}
}

func TestMeterConfig_Invalid(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := configClient(t, h)
	for _, tc := range []struct {
		name, message string
		kind          p4configv1.MeterSpec_Type
		config        meter.Config
	}{
		{"negative CIR", "CIR", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: -1}},
		{"negative CBurst", "CBurst", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CBurst: -2}},
		{"negative PIR", "PIR", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{PIR: math.MinInt64}},
		{"negative PBurst", "PBurst", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{PBurst: -1}},
		{"negative EBurst", "EBurst", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{EBurst: -1}},
		{"legacy all negative", "non-negative", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: -1, CBurst: -1, PIR: -1, PBurst: -1}},
		{"reversed rates", "at least CIR", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: 11, PIR: 10}},
		{"maximum rate mismatch", "at least CIR", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{CIR: math.MaxInt64, PIR: math.MaxInt64 - 1}},
		{"excess on two rate", "EBurst must be zero", p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, meter.Config{EBurst: 1}},
		{"single three rate mismatch", "CIR = PIR", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{CIR: 10, PIR: 20}},
		{"single three burst mismatch", "CBurst = PBurst", p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, meter.Config{CBurst: 10, PBurst: 20}},
		{"single two rate mismatch", "CIR = PIR", p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR, meter.Config{CIR: 10, PIR: 20}},
		{"single two burst mismatch", "CBurst = PBurst", p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR, meter.Config{CBurst: 10, PBurst: 20}},
		{"excess on single two", "EBurst must be zero", p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR, meter.Config{EBurst: 1}},
		{"unknown type", "unsupported meter type", p4configv1.MeterSpec_Type(99), meter.Config{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS, Type: tc.kind}, "")
			err := r.Write(ctx, "rate", 0, tc.config)
			require.ErrorContains(t, err, tc.message)
			require.ErrorContains(t, err, "rate")
		})
	}
	for _, spec := range []*p4configv1.MeterSpec{nil, {}, {Unit: p4configv1.MeterSpec_Unit(99)}} {
		r := configReader(t, c, spec, "")
		require.Error(t, r.Write(ctx, "rate", 0, meter.Config{}))
	}
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Empty(t, h.WriteRequests, "invalid configuration reached target")
}

func TestMeterConfig_ResetAndRead(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := configClient(t, h)
	for _, kind := range []p4configv1.MeterSpec_Type{p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR} {
		r := configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS, Type: kind}, "")
		require.NoError(t, r.Reset(ctx, "rate", 3))
		h.Mu.Lock()
		req := h.WriteRequests[len(h.WriteRequests)-1]
		h.Mu.Unlock()
		require.Len(t, req.Updates, 1)
		require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
		entry := req.Updates[0].Entity.GetMeterEntry()
		require.EqualValues(t, 0x14000001, entry.MeterId)
		require.EqualValues(t, 3, entry.GetIndex().GetIndex())
		require.Nil(t, entry.Config)
		require.Nil(t, entry.CounterData)
	}
	r := configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS}, "")
	h.Mu.Lock()
	h.WriteRequests = nil
	h.Mu.Unlock()
	require.Error(t, r.Reset(ctx, "missing", 0))
	for _, index := range []int64{math.MinInt64, -2, -1, 4, math.MaxInt64} {
		require.ErrorContains(t, r.Reset(ctx, "rate", index), "index")
	}
	h.Mu.Lock()
	require.Empty(t, h.WriteRequests)
	h.Mu.Unlock()
	r = configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS}, "TranslatedIndex")
	require.NoError(t, r.Reset(ctx, "rate", math.MaxInt64))
	h.Mu.Lock()
	entry := h.WriteRequests[0].Updates[0].Entity.GetMeterEntry()
	h.Mu.Unlock()
	require.EqualValues(t, math.MaxInt64, entry.GetIndex().GetIndex())
	want := &p4v1.MeterEntry{MeterId: 0x14000001, Index: &p4v1.Index{}, Config: &p4v1.MeterConfig{Cir: 10, Pir: 10, Cburst: 20, Pburst: 20, Eburst: 30}, CounterData: &p4v1.MeterCounterData{Green: &p4v1.CounterData{PacketCount: 7}}}
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{{Entity: &p4v1.Entity_MeterEntry{MeterEntry: want}}, {Entity: &p4v1.Entity_MeterEntry{MeterEntry: &p4v1.MeterEntry{MeterId: 0x14000001, Index: &p4v1.Index{Index: 1}}}}}}}
	h.Mu.Unlock()
	entries, err := r.Read(ctx, "rate", -1)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.True(t, proto.Equal(want, entries[0]))
	require.Nil(t, entries[1].Config)
}

func TestMeterConfig_TargetErrors(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := configClient(t, h)
	r := configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS, Type: p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR}, "")
	h.Mu.Lock()
	h.OverrideWriteErr = status.Error(codes.Unimplemented, "single-rate meters unavailable")
	h.Mu.Unlock()
	require.ErrorIs(t, r.Write(ctx, "rate", 0, meter.Config{EBurst: 1}), errs.ErrTargetUnsupported)
	require.ErrorIs(t, r.Reset(ctx, "rate", 0), errs.ErrTargetUnsupported)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.WriteRequests, 2, "failed writes must not be retried")
}

func TestMeterConfig_Concurrent(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := configClient(t, h)
	r := configReader(t, c, &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS, Type: p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR}, "")
	failures := make(chan error, 100)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				failures <- r.Reset(ctx, "rate", int64(i%4))
			} else {
				failures <- r.Write(ctx, "rate", int64(i%4), meter.Config{EBurst: int64(i)})
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.WriteRequests, 100)
}
