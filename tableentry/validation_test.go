package tableentry_test

import (
	"math"
	"math/big"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func validationPipeline(t *testing.T, change func(*p4configv1.Table)) *pipeline.Pipeline {
	t.Helper()
	info := proto.Clone(fixturePipeline(t).Info()).(*p4configv1.P4Info)
	info.Actions = append(info.Actions, &p4configv1.Action{
		Preamble: &p4configv1.Preamble{Id: 11, Name: "ingress.other", Alias: "other"},
	})
	change(info.Tables[0])
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestBuilder_ActionMembershipAndScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refs   []*p4configv1.ActionRef
		action string
		entry  string
		def    string
	}{
		{"both scopes", []*p4configv1.ActionRef{{Id: 10}}, "ingress.forward", "", ""},
		{"alias", []*p4configv1.ActionRef{{Id: 10}}, "forward", "", ""},
		{"table only", []*p4configv1.ActionRef{{Id: 10, Scope: p4configv1.ActionRef_TABLE_ONLY}}, "forward", "", "TABLE_ONLY"},
		{"default only", []*p4configv1.ActionRef{{Id: 10, Scope: p4configv1.ActionRef_DEFAULT_ONLY}}, "forward", "DEFAULT_ONLY", ""},
		{"no references", nil, "forward", "not on table", "not on table"},
		{"other table's action", []*p4configv1.ActionRef{{Id: 11}}, "forward", "not on table", "not on table"},
		{"unreferenced action", []*p4configv1.ActionRef{{Id: 10}}, "other", "not on table", "not on table"},
		{"unknown scope", []*p4configv1.ActionRef{{Id: 10, Scope: 99}}, "forward", "scope", "scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validationPipeline(t, func(table *p4configv1.Table) { table.ActionRefs = tc.refs })
			for _, def := range []bool{false, true} {
				b := tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1})).
					Action(tc.action, tableentry.Param("port", []byte{1}))
				if tc.action == "other" {
					b.Action(tc.action)
				}
				want := tc.entry
				if def {
					b.AsDefault().Match("unknown", tableentry.Exact([]byte{255})).Priority(-1)
					want = tc.def
				}
				entry, err := b.Build()
				if want != "" {
					require.ErrorContains(t, err, want, "default=%t", def)
					require.Nil(t, entry)
				} else {
					require.NoError(t, err, "default=%t", def)
					require.EqualValues(t, 10, entry.GetAction().GetAction().ActionId)
					if def {
						require.Empty(t, entry.Match)
						require.Zero(t, entry.Priority)
					}
				}
				key, err := b.BuildKey()
				require.NoError(t, err)
				require.Nil(t, key.Action, "key construction ignores action validity")
			}
		})
	}
}

func TestBuilder_ReadOnlyAndIndirectTables(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*p4configv1.Table)
		entry  string
		def    string
	}{
		{"constant entries", func(table *p4configv1.Table) { table.IsConstTable = true }, "constant table", ""},
		{"constant default", func(table *p4configv1.Table) { table.ConstDefaultActionId = 10 }, "", "constant default"},
		{"indirect", func(table *p4configv1.Table) { table.ImplementationId = 20 }, "indirect table", "indirect table"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validationPipeline(t, tc.change)
			for _, def := range []bool{false, true} {
				b := tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1})).
					Action("forward", tableentry.Param("port", []byte{1}))
				want := tc.entry
				if def {
					b.AsDefault()
					want = tc.def
				}
				entry, err := b.Build()
				if want == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, want)
					require.Nil(t, entry)
				}
				key, err := b.Action("unknown").IdleTimeout(-1).BuildKey()
				require.NoError(t, err)
				require.Nil(t, key.Action)
				require.Zero(t, key.IdleTimeoutNs)
			}
		})
	}
}

func TestBuilder_ConstantDefaultRejectsEveryAction(t *testing.T) {
	p := validationPipeline(t, func(table *p4configv1.Table) {
		table.ConstDefaultActionId = 10
		table.ActionRefs = append(table.ActionRefs, &p4configv1.ActionRef{Id: 11})
	})
	for _, action := range []string{"forward", "other"} {
		b := tableentry.NewBuilder(p, "ingress.t_exact").AsDefault().Action(action)
		if action == "forward" {
			b.Action(action, tableentry.Param("port", []byte{1}))
		}
		entry, err := b.Build()
		require.ErrorContains(t, err, "constant default")
		require.Nil(t, entry)
	}
}

func TestBuilder_IdleTimeoutValidation(t *testing.T) {
	for _, supported := range []bool{false, true} {
		p := validationPipeline(t, func(table *p4configv1.Table) {
			if supported {
				table.IdleTimeoutBehavior = p4configv1.Table_NOTIFY_CONTROL
			}
		})
		for _, def := range []bool{false, true} {
			for _, timeout := range []int64{-1, math.MinInt64, 0, 1, math.MaxInt64} {
				b := tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Exact([]byte{1})).
					Action("forward", tableentry.Param("port", []byte{1})).IdleTimeout(timeout).Metadata([]byte("cookie"))
				if def {
					b.AsDefault()
				}
				entry, err := b.Build()
				switch {
				case timeout < 0:
					require.ErrorContains(t, err, "negative")
				case def && timeout != 0:
					require.ErrorContains(t, err, "default")
				case !supported && timeout != 0:
					require.ErrorContains(t, err, "idle timeout")
				default:
					require.NoError(t, err)
					require.Equal(t, timeout, entry.IdleTimeoutNs)
					require.Equal(t, []byte("cookie"), entry.Metadata)
				}
				if err != nil {
					require.Nil(t, entry)
				}
				key, err := b.BuildKey()
				require.NoError(t, err)
				require.Zero(t, key.IdleTimeoutNs)
				require.Empty(t, key.Metadata)
			}
		}
	}
}

func TestBuilder_FullRangeWildcard(t *testing.T) {
	for _, width := range []int32{1, 7, 8, 9, 16, 64, 65, 128} {
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
		p := validationPipeline(t, func(table *p4configv1.Table) {
			table.MatchFields[0].Bitwidth = width
			table.MatchFields[0].Match = &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_RANGE}
		})
		for _, low := range [][]byte{nil, {}, {0}, {0, 0}} {
			high := append([]byte{0, 0}, max.Bytes()...)
			before := append([]byte(nil), high...)
			b := tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Range(low, high)).
				Action("forward", tableentry.Param("port", []byte{1})).Priority(10)
			entry, err := b.Build()
			require.NoError(t, err, "width=%d", width)
			require.Empty(t, roundTripEntry(t, entry).Match, "width=%d", width)
			require.EqualValues(t, 10, entry.Priority)
			require.Equal(t, before, high, "caller input must be preserved")
			key, err := b.BuildKey()
			require.NoError(t, err)
			require.Empty(t, key.Match)
			_, err = b.Priority(0).BuildKey()
			require.ErrorContains(t, err, "positive priority")
		}
		for _, tc := range []struct{ low, high []byte }{
			{[]byte{1}, max.Bytes()},
			{[]byte{0}, new(big.Int).Sub(max, big.NewInt(1)).Bytes()},
		} {
			entry, err := tableentry.NewBuilder(p, "ingress.t_exact").Match("hdr.eth.dst", tableentry.Range(tc.low, tc.high)).
				Action("forward", tableentry.Param("port", []byte{1})).Priority(10).Build()
			require.NoError(t, err)
			require.Len(t, entry.Match, 1, "a partial range must remain a concrete match")
		}
		_, err := tableentry.NewBuilder(p, "ingress.t_exact").
			Match("hdr.eth.dst", tableentry.Range(nil, new(big.Int).Lsh(big.NewInt(1), uint(width)).Bytes())).Priority(10).BuildKey()
		require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
	}
}

func TestBuilder_FullRangePreservesOtherMatches(t *testing.T) {
	b := tableentry.NewBuilder(fixturePipeline(t), "ingress.t_tcam").
		Match("hdr.ipv4.dst", tableentry.Ternary([]byte{1}, []byte{255})).
		Match("hdr.tcp.port", tableentry.Range(nil, []byte{0xff, 0xff})).
		Match("hdr.meta.tag", tableentry.Optional([]byte{0})).Priority(10)
	key, err := b.BuildKey()
	require.NoError(t, err)
	require.Len(t, key.Match, 2)
	require.EqualValues(t, 1, key.Match[0].FieldId)
	require.EqualValues(t, 3, key.Match[1].FieldId)
	require.NotNil(t, key.Match[1].GetOptional())
	require.Equal(t, []byte{0}, key.Match[1].GetOptional().Value)
	entry, err := b.Action("forward", tableentry.Param("port", []byte{1})).Build()
	require.NoError(t, err)
	entry.Action = nil
	require.True(t, proto.Equal(key, entry))
}

func TestBuilder_WidePartialRange(t *testing.T) {
	p := validationPipeline(t, func(table *p4configv1.Table) {
		table.MatchFields[0].Bitwidth = math.MaxInt32
		table.MatchFields[0].Match = &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_RANGE}
	})
	key, err := tableentry.NewBuilder(p, "ingress.t_exact").
		Match("hdr.eth.dst", tableentry.Range(nil, []byte{1})).Priority(10).BuildKey()
	require.NoError(t, err)
	require.Len(t, key.Match, 1)
	require.Equal(t, []byte{0}, key.Match[0].GetRange().Low)
	require.Equal(t, []byte{1}, key.Match[0].GetRange().High)
}
