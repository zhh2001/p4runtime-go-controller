package tableentry_test

import (
	"math"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func TestBuilder_BuildKey(t *testing.T) {
	info := proto.Clone(fixturePipeline(t).Info()).(*p4configv1.P4Info)
	for _, table := range info.Tables {
		table.IdleTimeoutBehavior = p4configv1.Table_NOTIFY_CONTROL
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		table    string
		field    string
		match    tableentry.MatchValue
		priority int32
	}{
		{"exact zero", "ingress.t_exact", "hdr.eth.dst", tableentry.Exact([]byte{0, 0}), 0},
		{"LPM", "ingress.t_lpm", "hdr.ipv4.dst", tableentry.LPM([]byte{10, 9, 8, 7}, 8), 0},
		{"LPM wildcard", "ingress.t_lpm", "hdr.ipv4.dst", tableentry.LPM([]byte{0}, 0), 0},
		{"ternary", "ingress.t_tcam", "hdr.ipv4.dst", tableentry.Ternary([]byte{255}, []byte{0, 255}), 10},
		{"ternary wildcard", "ingress.t_tcam", "hdr.ipv4.dst", tableentry.Ternary([]byte{0}, []byte{0}), 10},
		{"range", "ingress.t_tcam", "hdr.tcp.port", tableentry.Range([]byte{0, 1}, []byte{0, 3}), 20},
		{"optional zero", "ingress.t_tcam", "hdr.meta.tag", tableentry.Optional([]byte{0, 0}), 30},
		{"optional wildcard", "ingress.t_tcam", "hdr.meta.tag", tableentry.Optional(nil), math.MaxInt32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tableentry.NewBuilder(p, tc.table).Match(tc.field, tc.match).Priority(tc.priority)
			key, err := b.BuildKey()
			require.NoError(t, err)
			assert.Nil(t, key.Action)
			assert.Empty(t, key.Metadata)
			assert.Zero(t, key.IdleTimeoutNs)
			assert.Equal(t, tc.priority, key.Priority)
			_, err = b.Build()
			require.ErrorContains(t, err, "action required")
			entry, err := b.Action("forward", tableentry.Param("port", []byte{1})).
				Metadata([]byte("cookie")).IdleTimeout(100).Build()
			require.NoError(t, err)
			entry.Action, entry.Metadata, entry.IdleTimeoutNs = nil, nil, 0
			require.True(t, proto.Equal(entry, roundTripEntry(t, key)))
			keyWithAction, err := b.BuildKey()
			require.NoError(t, err)
			require.True(t, proto.Equal(key, keyWithAction))
		})
	}
}

func TestBuilder_BuildKeyIgnoresNonKeyFields(t *testing.T) {
	b := tableentry.NewBuilder(fixturePipeline(t), "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact([]byte{1})).
		Action("unknown", tableentry.Param("unknown", []byte{255})).
		Metadata([]byte("cookie")).IdleTimeout(-1)
	key, err := b.BuildKey()
	require.NoError(t, err)
	require.True(t, proto.Equal(&p4v1.TableEntry{TableId: 1, Match: []*p4v1.FieldMatch{{
		FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Exact_{Exact: &p4v1.FieldMatch_Exact{Value: []byte{1}}},
	}}}, key))
	_, err = b.IdleTimeout(0).Build()
	require.Error(t, err, "Build must still validate the action")
	key.Match[0].GetExact().Value[0] = 2
	key.Match[0].FieldId = 9
	again, err := b.BuildKey()
	require.NoError(t, err)
	assert.EqualValues(t, 1, again.Match[0].FieldId)
	assert.Equal(t, []byte{1}, again.Match[0].GetExact().Value)
}

func TestBuilder_BuildKeyDefault(t *testing.T) {
	key, err := tableentry.NewBuilder(fixturePipeline(t), "ingress.t_tcam").
		AsDefault().Match("unknown", tableentry.Exact([]byte{255})).Priority(-1).
		Metadata([]byte("cookie")).IdleTimeout(100).BuildKey()
	require.NoError(t, err)
	assert.True(t, proto.Equal(&p4v1.TableEntry{TableId: 3, IsDefaultAction: true}, key))
}

func TestBuilder_BuildKeyValidation(t *testing.T) {
	p := fixturePipeline(t)
	for _, tc := range []struct {
		name string
		b    *tableentry.Builder
		want string
	}{
		{"nil pipeline", tableentry.NewBuilder(nil, "table"), "nil pipeline"},
		{"unknown table", tableentry.NewBuilder(p, "unknown"), "not in pipeline"},
		{"missing exact", tableentry.NewBuilder(p, "ingress.t_exact"), "exact field"},
		{"unknown field", tableentry.NewBuilder(p, "ingress.t_lpm").Match("unknown", tableentry.Exact([]byte{1})), "not on table"},
		{"wrong kind", tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Optional([]byte{1})), "expects EXACT"},
		{"wide exact", tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1, 0, 0, 0, 0, 0, 0})), "bitwidth allows"},
		{"invalid prefix", tableentry.NewBuilder(p, "ingress.t_lpm").Match("hdr.ipv4.dst", tableentry.LPM([]byte{1}, 33)), "prefix"},
		{"reversed range", tableentry.NewBuilder(p, "ingress.t_tcam").Match("hdr.tcp.port", tableentry.Range([]byte{2}, []byte{1})).Priority(1), "low"},
		{"missing priority", tableentry.NewBuilder(p, "ingress.t_tcam"), "positive priority"},
		{"negative priority", tableentry.NewBuilder(p, "ingress.t_tcam").Priority(-1), "positive priority"},
		{"priority on exact", tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1})).Priority(1), "zero priority"},
		{"negative priority on exact", tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1})).Priority(-1), "zero priority"},
		{"priority on LPM", tableentry.NewBuilder(p, "ingress.t_lpm").Priority(1), "zero priority"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := tc.b.BuildKey()
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, key)
			entry, err := tc.b.Action("forward", tableentry.Param("port", []byte{1})).Build()
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, entry)
		})
	}
}

func TestBuilder_BuildKeyMixedMatches(t *testing.T) {
	info := proto.Clone(fixturePipeline(t).Info()).(*p4configv1.P4Info)
	table := info.Tables[2]
	table.MatchFields = []*p4configv1.MatchField{
		{Id: 7, Name: "ternary", Bitwidth: 9, Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_TERNARY}},
		{Id: 2, Name: "exact", Bitwidth: 9, Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_EXACT}},
		{Id: 5, Name: "optional", Bitwidth: 9, Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_OPTIONAL}},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	b := tableentry.NewBuilder(p, table.Preamble.Name).Match("ternary", tableentry.Ternary([]byte{3}, []byte{1})).
		Match("optional", tableentry.Optional([]byte{0})).Match("exact", tableentry.Exact([]byte{2})).Priority(10)
	key, err := b.BuildKey()
	require.NoError(t, err)
	require.Len(t, key.Match, 3)
	assert.Equal(t, []uint32{2, 5, 7}, []uint32{key.Match[0].FieldId, key.Match[1].FieldId, key.Match[2].FieldId})
	key, err = b.Match("ternary", tableentry.Ternary([]byte{0}, []byte{0})).Match("optional", tableentry.Optional(nil)).BuildKey()
	require.NoError(t, err)
	require.Len(t, key.Match, 1)
	assert.EqualValues(t, 2, key.Match[0].FieldId)
	assert.EqualValues(t, 10, key.Priority)
	_, err = b.Priority(0).BuildKey()
	require.ErrorContains(t, err, "positive priority")
	_, err = tableentry.NewBuilder(p, table.Preamble.Name).Priority(10).BuildKey()
	require.ErrorIs(t, err, errs.ErrInvalidMatchField)
}
