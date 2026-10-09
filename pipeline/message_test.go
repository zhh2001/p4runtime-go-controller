package pipeline_test

import (
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestNew_RejectsInvalidMessageGraph(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*p4configv1.P4Info)
		message string
	}{
		{"nil table", func(p *p4configv1.P4Info) { p.Tables[0] = nil }, "tables[0]: missing P4Info"},
		{"nil match field", func(p *p4configv1.P4Info) { p.Tables[0].MatchFields[0] = nil }, "match_fields[0]: missing P4Info"},
		{"nil metadata field", func(p *p4configv1.P4Info) { p.ControllerPacketMetadata[0].Metadata[0] = nil }, "metadata[0]: missing P4Info"},
		{"nil map value", func(p *p4configv1.P4Info) {
			p.TypeInfo = &p4configv1.P4TypeInfo{Structs: map[string]*p4configv1.P4StructTypeSpec{"Broken": nil}}
		}, "structs[Broken]: missing P4Info"},
		{"nil oneof message", func(p *p4configv1.P4Info) {
			p.Registers[0].TypeSpec = &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bool{}}
		}, "missing bool declaration"},
		{"typed nil oneof", func(p *p4configv1.P4Info) {
			var missing *p4configv1.P4DataTypeSpec_Bool
			p.Registers[0].TypeSpec = &p4configv1.P4DataTypeSpec{TypeSpec: missing}
		}, "missing P4Info oneof"},
		{"typed nil scalar oneof", func(p *p4configv1.P4Info) {
			var missing *p4configv1.MatchField_MatchType_
			p.Tables[0].MatchFields[0].Match = missing
		}, "missing P4Info oneof"},
		{"nil tuple member", func(p *p4configv1.P4Info) {
			p.Registers[0].TypeSpec = &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{Tuple: &p4configv1.P4TupleTypeSpec{Members: []*p4configv1.P4DataTypeSpec{nil}}}}
		}, "members[0]: missing P4Info"},
		{"pointer cycle", func(p *p4configv1.P4Info) {
			tuple := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{Tuple: &p4configv1.P4TupleTypeSpec{}}}
			tuple.GetTuple().Members = []*p4configv1.P4DataTypeSpec{tuple}
			p.Registers[0].TypeSpec = tuple
		}, "recursive P4Info message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := sampleInfo()
			tc.change(info)
			p, err := pipeline.New(info, nil)
			require.Nil(t, p)
			require.ErrorContains(t, err, "pipeline.New:")
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestNew_SharedMessagesAndSemanticValidation(t *testing.T) {
	info := sampleInfo()
	shared := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bool{Bool: &p4configv1.P4BoolType{}}}
	info.Registers[0].TypeSpec = &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{Tuple: &p4configv1.P4TupleTypeSpec{Members: []*p4configv1.P4DataTypeSpec{shared, shared}}}}
	info.Digests[0].TypeSpec = shared
	// Shape validation leaves numeric and resource rules to their API or target.
	info.Tables[0].MatchFields[0].Bitwidth = -1
	info.Counters[0].Size = -1
	info.Meters[0].Spec = nil
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	require.True(t, proto.Equal(info, p.Info()))
}

func TestPipeline_ProtoContentsAreCopied(t *testing.T) {
	info := sampleInfo()
	info.Counters[0].Preamble.Annotations = []string{"@name(\"original\")"}
	info.TypeInfo = &p4configv1.P4TypeInfo{SerializableEnums: map[string]*p4configv1.P4SerializableEnumTypeSpec{
		"Code": {UnderlyingType: &p4configv1.P4BitTypeSpec{Bitwidth: 9}, Members: []*p4configv1.P4SerializableEnumTypeSpec_Member{{Name: "OK", Value: []byte{1}}}},
	}}
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 100, protowire.VarintType), 7)
	info.ProtoReflect().SetUnknown(unknown)
	info.Counters[0].ProtoReflect().SetUnknown(append([]byte(nil), unknown...))
	want := proto.Clone(info)
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	info.TypeInfo.SerializableEnums["Code"].Members[0].Value[0] = 99
	info.Counters[0].Preamble.Annotations[0] = "changed"
	info.ProtoReflect().GetUnknown()[len(unknown)-1] = 99
	info.Counters[0].ProtoReflect().GetUnknown()[len(unknown)-1] = 99
	copy := p.Info()
	copy.TypeInfo.SerializableEnums["Code"].Members[0].Value[0] = 88
	delete(copy.TypeInfo.SerializableEnums, "Code")
	copy.ProtoReflect().GetUnknown()[len(unknown)-1] = 88
	counter, _ := p.Counter("pkt_counter")
	raw := counter.Raw()
	raw.Preamble.Annotations[0] = "changed"
	raw.ProtoReflect().GetUnknown()[len(unknown)-1] = 88
	require.True(t, proto.Equal(want, p.Info()))
	require.Equal(t, byte(7), counter.Raw().ProtoReflect().GetUnknown()[len(unknown)-1])
}
