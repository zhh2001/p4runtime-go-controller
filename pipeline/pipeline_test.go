package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func sampleInfo() *p4configv1.P4Info {
	return &p4configv1.P4Info{
		PkgInfo: &p4configv1.PkgInfo{Arch: "v1model"},
		Tables: []*p4configv1.Table{
			{
				Preamble: &p4configv1.Preamble{Id: 100, Name: "ingress.t_l2", Alias: "t_l2"},
				Size:     1024,
				MatchFields: []*p4configv1.MatchField{
					{
						Id:       1,
						Name:     "hdr.eth.dst",
						Bitwidth: 48,
						Match: &p4configv1.MatchField_MatchType_{
							MatchType: p4configv1.MatchField_EXACT,
						},
					},
					{
						Id:       2,
						Name:     "hdr.ipv4.dst",
						Bitwidth: 32,
						Match: &p4configv1.MatchField_MatchType_{
							MatchType: p4configv1.MatchField_LPM,
						},
					},
				},
				ActionRefs: []*p4configv1.ActionRef{{Id: 200}},
			},
		},
		Actions: []*p4configv1.Action{
			{
				Preamble: &p4configv1.Preamble{Id: 200, Name: "ingress.forward", Alias: "forward"},
				Params: []*p4configv1.Action_Param{
					{Id: 1, Name: "port", Bitwidth: 9},
				},
			},
		},
		Counters: []*p4configv1.Counter{
			{
				Preamble: &p4configv1.Preamble{Id: 300, Name: "ingress.pkt_counter", Alias: "pkt_counter"},
				Spec:     &p4configv1.CounterSpec{Unit: p4configv1.CounterSpec_PACKETS},
				Size:     64,
			},
		},
		DirectCounters: []*p4configv1.DirectCounter{
			{
				Preamble:      &p4configv1.Preamble{Id: 301, Name: "ingress.direct"},
				Spec:          &p4configv1.CounterSpec{Unit: p4configv1.CounterSpec_BYTES},
				DirectTableId: 100,
			},
		},
		Meters: []*p4configv1.Meter{
			{
				Preamble: &p4configv1.Preamble{Id: 400, Name: "ingress.meter"},
				Spec:     &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS},
				Size:     8,
			},
		},
		DirectMeters: []*p4configv1.DirectMeter{
			{
				Preamble:      &p4configv1.Preamble{Id: 401, Name: "ingress.direct_meter"},
				Spec:          &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_BYTES},
				DirectTableId: 100,
			},
		},
		Registers: []*p4configv1.Register{
			{
				Preamble: &p4configv1.Preamble{Id: 500, Name: "ingress.register"},
				Size:     16,
			},
		},
		Digests: []*p4configv1.Digest{
			{
				Preamble: &p4configv1.Preamble{Id: 600, Name: "ingress.digest"},
			},
		},
		ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{
			{
				Preamble: &p4configv1.Preamble{Id: 700, Name: "packet_in"},
				Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{
					{Id: 1, Name: "ingress_port", Bitwidth: 9},
				},
			},
		},
	}
}

func TestNew_Indexes(t *testing.T) {
	p, err := pipeline.New(sampleInfo(), []byte("device-cfg"))
	require.NoError(t, err)

	// Table by name, alias, and ID.
	tbl, ok := p.Table("ingress.t_l2")
	require.True(t, ok)
	assert.Equal(t, uint32(100), tbl.ID)
	assert.Equal(t, int64(1024), tbl.Size)
	assert.Len(t, tbl.MatchFields, 2)

	tbl2, ok := p.Table("t_l2")
	require.True(t, ok)
	assert.Same(t, tbl, tbl2)

	tbl3, ok := p.TableByID(100)
	require.True(t, ok)
	assert.Same(t, tbl, tbl3)

	// Match field lookups.
	mf, ok := tbl.MatchField("hdr.eth.dst")
	require.True(t, ok)
	assert.Equal(t, int32(48), mf.Bitwidth)
	assert.Equal(t, p4configv1.MatchField_EXACT, mf.MatchType)

	mfByID, ok := tbl.MatchFieldByID(2)
	require.True(t, ok)
	assert.Equal(t, "hdr.ipv4.dst", mfByID.Name)
	assert.Equal(t, p4configv1.MatchField_LPM, mfByID.MatchType)

	// Action lookup.
	act, ok := p.Action("ingress.forward")
	require.True(t, ok)
	assert.Len(t, act.Params, 1)
	prm, ok := act.Param("port")
	require.True(t, ok)
	assert.Equal(t, int32(9), prm.Bitwidth)

	prmByID, ok := act.ParamByID(1)
	require.True(t, ok)
	assert.Same(t, prm, prmByID)

	actAlias, ok := p.Action("forward")
	require.True(t, ok)
	assert.Same(t, act, actAlias)

	actByID, ok := p.ActionByID(200)
	require.True(t, ok)
	assert.Same(t, act, actByID)

	// Counters, meters, registers, digests, packet metadata.
	cnt, ok := p.Counter("ingress.pkt_counter")
	require.True(t, ok)
	assert.Equal(t, int64(64), cnt.Size)

	_, ok = p.Counter("pkt_counter")
	require.True(t, ok)

	_, ok = p.CounterByID(300)
	require.True(t, ok)

	dc, ok := p.DirectCounter("ingress.direct")
	require.True(t, ok)
	assert.Equal(t, "ingress.t_l2", dc.DirectTableName)

	_, ok = p.Meter("ingress.meter")
	require.True(t, ok)
	_, ok = p.DirectMeter("ingress.direct_meter")
	require.True(t, ok)
	_, ok = p.Register("ingress.register")
	require.True(t, ok)
	_, ok = p.Digest("ingress.digest")
	require.True(t, ok)

	pm, ok := p.PacketMetadata("packet_in")
	require.True(t, ok)
	f, ok := pm.Field("ingress_port")
	require.True(t, ok)
	assert.Equal(t, int32(9), f.Bitwidth)

	_, ok = p.PacketMetadataByID(700)
	require.True(t, ok)
	_, ok = pm.FieldByID(1)
	require.True(t, ok)
}

func TestNew_RejectsNil(t *testing.T) {
	_, err := pipeline.New(nil, nil)
	assert.Error(t, err)
}

func TestLoad_Binary(t *testing.T) {
	info := sampleInfo()
	blob, err := proto.Marshal(info)
	require.NoError(t, err)
	p, err := pipeline.Load(blob, []byte("cfg"))
	require.NoError(t, err)
	_, ok := p.Table("ingress.t_l2")
	assert.True(t, ok)
	assert.Equal(t, []byte("cfg"), p.DeviceConfig())
}

func TestLoad_BinaryInvalid(t *testing.T) {
	_, err := pipeline.Load([]byte{0xff, 0xff}, nil)
	assert.Error(t, err)
}

func TestLoadText(t *testing.T) {
	info := sampleInfo()
	blob, err := prototext.Marshal(info)
	require.NoError(t, err)
	p, err := pipeline.LoadText(blob, nil)
	require.NoError(t, err)
	_, ok := p.Action("ingress.forward")
	assert.True(t, ok)
	assert.Nil(t, p.DeviceConfig())
}

func TestLoadText_Invalid(t *testing.T) {
	_, err := pipeline.LoadText([]byte("not a valid textproto {{"), nil)
	assert.Error(t, err)
}

func TestTables(t *testing.T) {
	p, err := pipeline.New(sampleInfo(), nil)
	require.NoError(t, err)
	all := p.Tables()
	require.Len(t, all, 1)
	assert.Equal(t, "ingress.t_l2", all[0].Name)
}

func TestLookups_NilReceiver(t *testing.T) {
	var p *pipeline.Pipeline
	_, ok := p.Table("anything")
	assert.False(t, ok)
	_, ok = p.TableByID(1)
	assert.False(t, ok)
	_, ok = p.Action("anything")
	assert.False(t, ok)

	var td *pipeline.TableDef
	_, ok = td.MatchField("anything")
	assert.False(t, ok)
}
