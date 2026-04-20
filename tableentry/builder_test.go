package tableentry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func fixturePipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Tables: []*p4configv1.Table{{
			Preamble: &p4configv1.Preamble{Id: 1, Name: "ingress.t_exact"},
			Size:     1024,
			MatchFields: []*p4configv1.MatchField{{
				Id:       1,
				Name:     "hdr.eth.dst",
				Bitwidth: 48,
				Match:    &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_EXACT},
			}},
			ActionRefs: []*p4configv1.ActionRef{{Id: 10}},
		}, {
			Preamble: &p4configv1.Preamble{Id: 2, Name: "ingress.t_lpm"},
			Size:     1024,
			MatchFields: []*p4configv1.MatchField{{
				Id: 1, Name: "hdr.ipv4.dst", Bitwidth: 32,
				Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_LPM},
			}},
			ActionRefs: []*p4configv1.ActionRef{{Id: 10}},
		}, {
			Preamble: &p4configv1.Preamble{Id: 3, Name: "ingress.t_tcam"},
			Size:     256,
			MatchFields: []*p4configv1.MatchField{{
				Id: 1, Name: "hdr.ipv4.dst", Bitwidth: 32,
				Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_TERNARY},
			}, {
				Id: 2, Name: "hdr.tcp.port", Bitwidth: 16,
				Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_RANGE},
			}, {
				Id: 3, Name: "hdr.meta.tag", Bitwidth: 8,
				Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_OPTIONAL},
			}},
			ActionRefs: []*p4configv1.ActionRef{{Id: 10}},
		}},
		Actions: []*p4configv1.Action{{
			Preamble: &p4configv1.Preamble{Id: 10, Name: "ingress.forward", Alias: "forward"},
			Params: []*p4configv1.Action_Param{
				{Id: 1, Name: "port", Bitwidth: 9},
			},
		}},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestBuilder_Exact(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("ingress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	require.NoError(t, err)
	assert.EqualValues(t, 1, entry.TableId)
	require.Len(t, entry.Match, 1)
	exact := entry.Match[0].GetExact()
	require.NotNil(t, exact)
	assert.Equal(t, []byte{0x11, 0x22, 0x33, 0x44, 0x55}, exact.Value)
	act := entry.GetAction().GetAction()
	require.NotNil(t, act)
	assert.EqualValues(t, 10, act.ActionId)
	require.Len(t, act.Params, 1)
	assert.Equal(t, []byte{0x01}, act.Params[0].Value)
}

func TestBuilder_LPM(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_lpm").
		Match("hdr.ipv4.dst", tableentry.LPM(codec.MustIPv4("10.0.0.0"), 8)).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(2, 9))).
		Build()
	require.NoError(t, err)
	lpm := entry.Match[0].GetLpm()
	require.NotNil(t, lpm)
	assert.EqualValues(t, 8, lpm.PrefixLen)
	assert.Equal(t, []byte{0x0a, 0x00, 0x00, 0x00}, lpm.Value)
}

func TestBuilder_LPMPrefixZeroSkipsField(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_lpm").
		Match("hdr.ipv4.dst", tableentry.LPM(codec.MustIPv4("10.0.0.0"), 0)).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	require.NoError(t, err)
	assert.Empty(t, entry.Match)
}

func TestBuilder_Ternary_Range_Optional_RequirePriority(t *testing.T) {
	p := fixturePipeline(t)
	b := tableentry.NewBuilder(p, "ingress.t_tcam").
		Match("hdr.ipv4.dst", tableentry.Ternary(
			codec.MustIPv4("10.0.0.0"),
			[]byte{0xff, 0xff, 0xff, 0x00})).
		Match("hdr.tcp.port", tableentry.Range(
			codec.MustEncodeUint(80, 16),
			codec.MustEncodeUint(443, 16))).
		Match("hdr.meta.tag", tableentry.Optional(codec.MustEncodeUint(7, 8))).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(3, 9)))

	_, err := b.Build()
	require.Error(t, err, "priority is required on ternary/range/optional tables")

	entry, err := b.Priority(10).Build()
	require.NoError(t, err)
	assert.EqualValues(t, 10, entry.Priority)
	require.Len(t, entry.Match, 3)

	// Verify the match types came through correctly.
	kinds := map[uint32]string{}
	for _, m := range entry.Match {
		switch m.GetFieldMatchType().(type) {
		case *p4v1.FieldMatch_Ternary_:
			kinds[m.FieldId] = "ternary"
		case *p4v1.FieldMatch_Range_:
			kinds[m.FieldId] = "range"
		case *p4v1.FieldMatch_Optional_:
			kinds[m.FieldId] = "optional"
		}
	}
	assert.Equal(t, map[uint32]string{1: "ternary", 2: "range", 3: "optional"}, kinds)
}

func TestBuilder_OptionalNilIsDontCare(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_tcam").
		Match("hdr.ipv4.dst", tableentry.Ternary(
			codec.MustIPv4("10.0.0.0"),
			[]byte{0xff, 0xff, 0xff, 0x00})).
		Match("hdr.tcp.port", tableentry.Range(
									codec.MustEncodeUint(80, 16),
									codec.MustEncodeUint(443, 16))).
		Match("hdr.meta.tag", tableentry.Optional(nil)). // don't-care
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Priority(5).
		Build()
	require.NoError(t, err)
	for _, m := range entry.Match {
		_, isOptional := m.GetFieldMatchType().(*p4v1.FieldMatch_Optional_)
		assert.False(t, isOptional, "optional with nil should be omitted")
	}
}

func TestBuilder_TernaryZeroMaskSkipsField(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_tcam").
		Match("hdr.ipv4.dst", tableentry.Ternary(
			codec.MustIPv4("10.0.0.0"),
			[]byte{0x00, 0x00, 0x00, 0x00})).
		Match("hdr.tcp.port", tableentry.Range(
			codec.MustEncodeUint(80, 16),
			codec.MustEncodeUint(443, 16))).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Priority(5).
		Build()
	require.NoError(t, err)
	for _, m := range entry.Match {
		_, isTernary := m.GetFieldMatchType().(*p4v1.FieldMatch_Ternary_)
		assert.False(t, isTernary, "ternary with zero mask should be omitted")
	}
}

func TestBuilder_DefaultAction(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_exact").
		AsDefault().
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	require.NoError(t, err)
	assert.True(t, entry.IsDefaultAction)
	assert.Empty(t, entry.Match)
}

func TestBuilder_IdleTimeoutAndMetadata(t *testing.T) {
	p := fixturePipeline(t)
	entry, err := tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		IdleTimeout(1_000_000).
		Metadata([]byte("cookie")).
		Build()
	require.NoError(t, err)
	assert.EqualValues(t, 1_000_000, entry.IdleTimeoutNs)
	assert.Equal(t, []byte("cookie"), entry.Metadata)
}

func TestBuilder_Errors(t *testing.T) {
	p := fixturePipeline(t)

	// unknown table
	_, err := tableentry.NewBuilder(p, "nope").Action("forward",
		tableentry.Param("port", codec.MustEncodeUint(1, 9))).Build()
	assert.Error(t, err)

	// nil pipeline
	_, err = tableentry.NewBuilder(nil, "anything").Build()
	assert.Error(t, err)

	// missing action
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Build()
	assert.Error(t, err)

	// wrong match kind
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.LPM(codec.MustMAC("00:11:22:33:44:55"), 48)).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	assert.ErrorIs(t, err, errs.ErrUnsupportedMatchKind)

	// unknown field
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("nope", tableentry.Exact([]byte{0x01})).
		Action("forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	assert.ErrorIs(t, err, errs.ErrInvalidMatchField)

	// missing action param
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("forward").
		Build()
	assert.ErrorIs(t, err, errs.ErrInvalidActionParam)

	// unknown action
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("unknown").
		Build()
	assert.Error(t, err)

	// unknown action param
	_, err = tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("forward",
			tableentry.Param("port", codec.MustEncodeUint(1, 9)),
			tableentry.Param("bogus", []byte{0x01})).
		Build()
	assert.ErrorIs(t, err, errs.ErrInvalidActionParam)
}

func TestBuilder_MatchValueEnum(t *testing.T) {
	// Ensure every marker implementation satisfies the interface and is
	// reachable via type switches. Cheap invariant, useful if we later add
	// a new kind.
	var _ tableentry.MatchValue = tableentry.ExactMatch{}
	var _ tableentry.MatchValue = tableentry.LPMMatch{}
	var _ tableentry.MatchValue = tableentry.TernaryMatch{}
	var _ tableentry.MatchValue = tableentry.RangeMatch{}
	var _ tableentry.MatchValue = tableentry.OptionalMatch{}
}
