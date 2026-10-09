package register_test

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
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/register"
)

func bitType(width int32) *p4configv1.P4DataTypeSpec {
	return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Bit{Bit: &p4configv1.P4BitTypeSpec{Bitwidth: width}}}}}
}
func intType(width int32) *p4configv1.P4DataTypeSpec {
	return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Int{Int: &p4configv1.P4IntTypeSpec{Bitwidth: width}}}}}
}
func varbitType(width int32) *p4configv1.P4DataTypeSpec {
	return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Varbit{Varbit: &p4configv1.P4VarbitTypeSpec{MaxBitwidth: width}}}}}
}
func varbitData(width int32, value ...byte) *p4v1.P4Data {
	return &p4v1.P4Data{Data: &p4v1.P4Data_Varbit{Varbit: &p4v1.P4Varbit{Bitwidth: width, Bitstring: value}}}
}
func boolType() *p4configv1.P4DataTypeSpec {
	return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bool{Bool: &p4configv1.P4BoolType{}}}
}
func tupleType(members ...*p4configv1.P4DataTypeSpec) *p4configv1.P4DataTypeSpec {
	return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{Tuple: &p4configv1.P4TupleTypeSpec{Members: members}}}
}
func bitData(value ...byte) *p4v1.P4Data {
	return &p4v1.P4Data{Data: &p4v1.P4Data_Bitstring{Bitstring: value}}
}
func boolData(value bool) *p4v1.P4Data {
	return &p4v1.P4Data{Data: &p4v1.P4Data_Bool{Bool: value}}
}
func tupleData(members ...*p4v1.P4Data) *p4v1.P4Data {
	return &p4v1.P4Data{Data: &p4v1.P4Data_Tuple{Tuple: &p4v1.P4StructLike{Members: members}}}
}
func namedType(kind, name string) *p4configv1.P4DataTypeSpec {
	ref := &p4configv1.P4NamedType{Name: name}
	switch kind {
	case "struct":
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Struct{Struct: ref}}
	case "header":
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Header{Header: ref}}
	case "union":
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_HeaderUnion{HeaderUnion: ref}}
	case "enum":
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Enum{Enum: ref}}
	case "serializable enum":
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_SerializableEnum{SerializableEnum: ref}}
	default:
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_NewType{NewType: ref}}
	}
}
func dataTypes() *p4configv1.P4TypeInfo {
	return &p4configv1.P4TypeInfo{
		Structs:           map[string]*p4configv1.P4StructTypeSpec{"Record": {Members: []*p4configv1.P4StructTypeSpec_Member{{Name: "count", TypeSpec: bitType(9)}, {Name: "enabled", TypeSpec: boolType()}, {Name: "delta", TypeSpec: intType(9)}}}},
		Headers:           map[string]*p4configv1.P4HeaderTypeSpec{"H": {Members: []*p4configv1.P4HeaderTypeSpec_Member{{Name: "count", TypeSpec: bitType(9).GetBitstring()}, {Name: "delta", TypeSpec: intType(9).GetBitstring()}}}},
		HeaderUnions:      map[string]*p4configv1.P4HeaderUnionTypeSpec{"U": {Members: []*p4configv1.P4HeaderUnionTypeSpec_Member{{Name: "h", Header: &p4configv1.P4NamedType{Name: "H"}}}}},
		Enums:             map[string]*p4configv1.P4EnumTypeSpec{"Color": {Members: []*p4configv1.P4EnumTypeSpec_Member{{Name: "GREEN"}, {Name: "RED"}}}},
		SerializableEnums: map[string]*p4configv1.P4SerializableEnumTypeSpec{"Code": {UnderlyingType: &p4configv1.P4BitTypeSpec{Bitwidth: 9}, Members: []*p4configv1.P4SerializableEnumTypeSpec_Member{{Name: "OK", Value: []byte{0}}}}},
		Error:             &p4configv1.P4ErrorTypeSpec{Members: []string{"NoError", "PacketTooShort"}},
		NewTypes: map[string]*p4configv1.P4NewTypeSpec{
			"Word":     {Representation: &p4configv1.P4NewTypeSpec_OriginalType{OriginalType: bitType(9)}},
			"Outer":    {Representation: &p4configv1.P4NewTypeSpec_OriginalType{OriginalType: namedType("new", "Word")}},
			"Port":     {Representation: &p4configv1.P4NewTypeSpec_TranslatedType{TranslatedType: &p4configv1.P4NewTypeTranslation{Uri: "test/port", SdnType: &p4configv1.P4NewTypeTranslation_SdnBitwidth{SdnBitwidth: 32}}}},
			"PortName": {Representation: &p4configv1.P4NewTypeSpec_TranslatedType{TranslatedType: &p4configv1.P4NewTypeTranslation{Uri: "test/name", SdnType: &p4configv1.P4NewTypeTranslation_SdnString_{SdnString: &p4configv1.P4NewTypeTranslation_SdnString{}}}}},
		},
	}
}
func dataPipeline(spec *p4configv1.P4DataTypeSpec, types *p4configv1.P4TypeInfo) (*pipeline.Pipeline, error) {
	return pipeline.New(&p4configv1.P4Info{TypeInfo: types, Registers: []*p4configv1.Register{{Preamble: &p4configv1.Preamble{Id: 0x16000001, Name: "ingress.state", Alias: "state"}, Size: 8, TypeSpec: spec}}}, nil)
}
func dataReader(t *testing.T, c *client.Client, spec *p4configv1.P4DataTypeSpec, types *p4configv1.P4TypeInfo) *register.Reader {
	t.Helper()
	p, err := dataPipeline(spec, types)
	require.NoError(t, err)
	r, err := register.NewReader(c, p)
	require.NoError(t, err)
	return r
}
func dataClient(t *testing.T, h *testutil.ServerHarness, options ...grpc.DialOption) (context.Context, *client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	options = append(options, grpc.WithContextDialer(h.Dialer()))
	c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(options...))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return ctx, c
}

func TestRegisterData_Values(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	validHeader := &p4v1.P4Header{IsValid: true, Bitstrings: [][]byte{{0, 1}, {0xff, 0xff}}}
	wantHeader := &p4v1.P4Header{IsValid: true, Bitstrings: [][]byte{{1}, {0xff}}}
	validUnion := &p4v1.P4HeaderUnion{ValidHeaderName: "h", ValidHeader: validHeader}
	wantUnion := &p4v1.P4HeaderUnion{ValidHeaderName: "h", ValidHeader: wantHeader}
	stackType := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_HeaderStack{HeaderStack: &p4configv1.P4HeaderStackTypeSpec{Header: &p4configv1.P4NamedType{Name: "H"}, Size: 2}}}
	unionStackType := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_HeaderUnionStack{HeaderUnionStack: &p4configv1.P4HeaderUnionStackTypeSpec{HeaderUnion: &p4configv1.P4NamedType{Name: "U"}, Size: 2}}}
	for _, tc := range []struct {
		name        string
		spec        *p4configv1.P4DataTypeSpec
		value, want *p4v1.P4Data
	}{
		{"unsigned zero", bitType(9), bitData(), bitData(0)},
		{"unsigned padding", bitType(9), bitData(0, 1, 255), bitData(1, 255)},
		{"unsigned high bit", bitType(8), bitData(255), bitData(255)},
		{"zero width", bitType(0), bitData(0, 0), bitData(0)},
		{"signed zero", intType(9), bitData(), bitData(0)},
		{"signed positive", intType(9), bitData(0, 0, 128), bitData(0, 128)},
		{"signed negative", intType(9), bitData(255, 255, 127), bitData(255, 127)},
		{"signed minimum", intType(9), bitData(255, 0), bitData(255, 0)},
		{"signed maximum", intType(9), bitData(0, 255), bitData(0, 255)},
		{"signed one bit", intType(1), bitData(255, 255), bitData(255)},
		{"varbit leading zeros", varbitType(32), varbitData(16, 0, 1), varbitData(16, 0, 1)},
		{"varbit non-byte width", varbitType(32), varbitData(9, 1, 255), varbitData(9, 1, 255)},
		{"varbit zero value", varbitType(32), varbitData(16, 0, 0), varbitData(16, 0, 0)},
		{"varbit empty", varbitType(32), varbitData(0), varbitData(0)},
		{"varbit zero maximum", varbitType(0), varbitData(0), varbitData(0)},
		{"nested varbit", tupleType(varbitType(32), bitType(9)), tupleData(varbitData(16, 0, 1), bitData(0, 1)), tupleData(varbitData(16, 0, 1), bitData(1))},
		{"boolean false", boolType(), boolData(false), boolData(false)},
		{"boolean true", boolType(), boolData(true), boolData(true)},
		{"tuple", tupleType(bitType(9), boolType(), intType(9)), tupleData(bitData(0, 1), boolData(false), bitData(255, 255)), tupleData(bitData(1), boolData(false), bitData(255))},
		{"struct", namedType("struct", "Record"), &p4v1.P4Data{Data: &p4v1.P4Data_Struct{Struct: &p4v1.P4StructLike{Members: []*p4v1.P4Data{bitData(0, 1), boolData(false), bitData(255, 255)}}}}, &p4v1.P4Data{Data: &p4v1.P4Data_Struct{Struct: &p4v1.P4StructLike{Members: []*p4v1.P4Data{bitData(1), boolData(false), bitData(255)}}}}},
		{"header", namedType("header", "H"), &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: validHeader}}, &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: wantHeader}}},
		{"invalid header", namedType("header", "H"), &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{}}}, &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{}}}},
		{"union", namedType("union", "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: validUnion}}, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: wantUnion}}},
		{"invalid union", namedType("union", "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: &p4v1.P4HeaderUnion{}}}, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: &p4v1.P4HeaderUnion{}}}},
		{"header stack", stackType, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{Entries: []*p4v1.P4Header{{}, validHeader}}}}, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{Entries: []*p4v1.P4Header{{}, wantHeader}}}}},
		{"union stack", unionStackType, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{Entries: []*p4v1.P4HeaderUnion{{}, validUnion}}}}, &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{Entries: []*p4v1.P4HeaderUnion{{}, wantUnion}}}}},
		{"enum", namedType("enum", "Color"), &p4v1.P4Data{Data: &p4v1.P4Data_Enum{Enum: "GREEN"}}, &p4v1.P4Data{Data: &p4v1.P4Data_Enum{Enum: "GREEN"}}},
		{"unnamed enum value", namedType("serializable enum", "Code"), &p4v1.P4Data{Data: &p4v1.P4Data_EnumValue{EnumValue: []byte{0, 255}}}, &p4v1.P4Data{Data: &p4v1.P4Data_EnumValue{EnumValue: []byte{255}}}},
		{"error", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Error{Error: &p4configv1.P4ErrorType{}}}, &p4v1.P4Data{Data: &p4v1.P4Data_Error{Error: "NoError"}}, &p4v1.P4Data{Data: &p4v1.P4Data_Error{Error: "NoError"}}},
		{"alias chain", namedType("new", "Outer"), bitData(0, 1), bitData(1)},
		{"translated number", namedType("new", "Port"), bitData(0, 255, 255, 255, 255), bitData(255, 255, 255, 255)},
		{"translated string", namedType("new", "PortName"), bitData(0, 'e', 't', 'h', '0', 255), bitData(0, 'e', 't', 'h', '0', 255)},
		{"empty translated string", namedType("new", "PortName"), bitData(), bitData()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := proto.Clone(tc.value)
			r := dataReader(t, c, tc.spec, dataTypes())
			require.NoError(t, r.WriteData(ctx, "state", 0, tc.value))
			require.True(t, proto.Equal(before, tc.value), "caller value changed")
			h.Mu.Lock()
			req := h.WriteRequests[len(h.WriteRequests)-1]
			h.Mu.Unlock()
			require.Len(t, req.Updates, 1)
			require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
			entry := req.Updates[0].Entity.GetRegisterEntry()
			require.EqualValues(t, 0x16000001, entry.RegisterId)
			require.NotNil(t, entry.Index)
			require.Zero(t, entry.Index.Index)
			require.True(t, proto.Equal(tc.want, entry.Data), "wire value: %s", entry.Data)
		})
	}
}

func TestRegisterData_InvalidValues(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	var nilData *p4v1.P4Data_Bool
	var nilSpec *p4configv1.P4DataTypeSpec_Bool
	for _, tc := range []struct {
		name    string
		spec    *p4configv1.P4DataTypeSpec
		value   *p4v1.P4Data
		message string
	}{
		{"nil value", bitType(9), nil, "missing P4Data"},
		{"unset value", bitType(9), &p4v1.P4Data{}, "missing P4Data"},
		{"typed nil value", boolType(), &p4v1.P4Data{Data: nilData}, "missing P4Data"},
		{"missing type", nil, bitData(1), "missing P4Info"},
		{"unset type", &p4configv1.P4DataTypeSpec{}, bitData(1), "missing P4Info"},
		{"typed nil type", &p4configv1.P4DataTypeSpec{TypeSpec: nilSpec}, boolData(false), "missing P4Info"},
		{"bool without declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bool{}}, boolData(false), "missing bool"},
		{"wrong boolean field", boolType(), bitData(1), "P4Data.bool"},
		{"wrong numeric field", bitType(9), boolData(false), "P4Data.bitstring"},
		{"unsigned overflow", bitType(9), bitData(2, 0), "width"},
		{"negative width", bitType(-1), bitData(0), "bitwidth"},
		{"zero width overflow", bitType(0), bitData(1), "bit<0>"},
		{"signed positive overflow", intType(9), bitData(1, 0), "int<9>"},
		{"signed negative overflow", intType(9), bitData(254, 255), "int<9>"},
		{"signed zero width", intType(0), bitData(0), "positive"},
		{"signed width one positive", intType(1), bitData(1), "int<1>"},
		{"varbit wrong field", varbitType(32), bitData(1), "P4Data.varbit"},
		{"varbit missing value", varbitType(32), &p4v1.P4Data{Data: &p4v1.P4Data_Varbit{}}, "P4Data.varbit"},
		{"varbit negative width", varbitType(32), varbitData(-1), "bitwidth"},
		{"varbit over maximum", varbitType(8), varbitData(9, 0, 1), "bitwidth"},
		{"varbit negative maximum", varbitType(-1), varbitData(0), "bitwidth"},
		{"varbit too few bytes", varbitType(32), varbitData(16, 1), "requires 2 bytes"},
		{"varbit too many bytes", varbitType(32), varbitData(8, 0, 1), "requires 1 bytes"},
		{"varbit empty has byte", varbitType(32), varbitData(0, 0), "requires 0 bytes"},
		{"varbit non-byte overflow", varbitType(32), varbitData(9, 2, 0), "width"},
		{"varbit missing declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Varbit{}}}}, varbitData(0), "missing varbit"},
		{"missing bitstring declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{}}, bitData(1), "missing bitstring"},
		{"missing bit declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Bit{}}}}, bitData(1), "missing bit"},
		{"missing int declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Int{}}}}, bitData(1), "missing int"},
		{"tuple length", tupleType(bitType(9)), tupleData(), "members"},
		{"tuple member type", tupleType(bitType(9)), tupleData(boolData(false)), "member 0"},
		{"tuple nil member", tupleType(bitType(9)), tupleData(nil), "missing P4Data"},
		{"tuple nil declaration", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{}}, tupleData(), "missing tuple"},
		{"tuple wrong field", tupleType(bitType(9)), &p4v1.P4Data{Data: &p4v1.P4Data_Struct{Struct: &p4v1.P4StructLike{Members: []*p4v1.P4Data{bitData(1)}}}}, "members"},
		{"struct length", namedType("struct", "Record"), &p4v1.P4Data{Data: &p4v1.P4Data_Struct{Struct: &p4v1.P4StructLike{}}}, "members"},
		{"unknown enum member", namedType("enum", "Color"), &p4v1.P4Data{Data: &p4v1.P4Data_Enum{Enum: "BLUE"}}, "declared"},
		{"enum wrong field", namedType("enum", "Color"), bitData(1), "P4Data.enum"},
		{"enum value wrong field", namedType("serializable enum", "Code"), bitData(1), "enum_value"},
		{"enum value overflow", namedType("serializable enum", "Code"), &p4v1.P4Data{Data: &p4v1.P4Data_EnumValue{EnumValue: []byte{2, 0}}}, "width"},
		{"translated overflow", namedType("new", "Port"), bitData(1, 0, 0, 0, 0), "width"},
		{"translated wrong field", namedType("new", "PortName"), boolData(false), "P4Data.bitstring"},
		{"invalid header fields", namedType("header", "H"), &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{Bitstrings: [][]byte{{1}}}}}, "must have no fields"},
		{"header missing field", namedType("header", "H"), &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{IsValid: true}}}, "requires 2 fields"},
		{"header field overflow", namedType("header", "H"), &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{IsValid: true, Bitstrings: [][]byte{{2, 0}, {0}}}}}, "field 0"},
		{"header wrong field", namedType("header", "H"), bitData(1), "P4Data.header"},
		{"union missing header", namedType("union", "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: &p4v1.P4HeaderUnion{ValidHeaderName: "h"}}}, "valid declared header"},
		{"union unknown member", namedType("union", "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: &p4v1.P4HeaderUnion{ValidHeaderName: "other", ValidHeader: &p4v1.P4Header{IsValid: true}}}}, "valid declared header"},
		{"invalid union has header", namedType("union", "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnion{HeaderUnion: &p4v1.P4HeaderUnion{ValidHeader: &p4v1.P4Header{}}}}, "must have no valid_header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := dataPipeline(tc.spec, dataTypes())
			if err == nil {
				r, readerErr := register.NewReader(c, p)
				require.NoError(t, readerErr)
				err = r.WriteData(ctx, "state", 0, tc.value)
			}
			require.ErrorContains(t, err, tc.message)
			h.Mu.Lock()
			defer h.Mu.Unlock()
			require.Empty(t, h.WriteRequests, "invalid value reached target")
		})
	}
	for _, kind := range []string{"struct", "header", "union", "enum", "serializable enum", "new"} {
		r := dataReader(t, c, namedType(kind, "missing"), dataTypes())
		require.ErrorContains(t, r.WriteData(ctx, "state", 0, bitData(0)), "unknown")
	}
}

func TestRegisterWrite_IntegerConvenience(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	for _, spec := range []*p4configv1.P4DataTypeSpec{bitType(9), intType(9), namedType("new", "Word"), namedType("new", "Port")} {
		r := dataReader(t, c, spec, dataTypes())
		require.NoError(t, r.Write(ctx, "state", 0, nil))
		h.Mu.Lock()
		entry := h.WriteRequests[len(h.WriteRequests)-1].Updates[0].Entity.GetRegisterEntry()
		h.Mu.Unlock()
		require.Equal(t, []byte{0}, entry.GetData().GetBitstring())
		require.ErrorIs(t, r.Write(ctx, "state", 0, []byte{1, 0, 0, 0, 0}), errs.ErrInvalidBitWidth)
	}
	for _, spec := range []*p4configv1.P4DataTypeSpec{varbitType(32), boolType(), tupleType(bitType(9)), namedType("enum", "Color"), namedType("serializable enum", "Code"), namedType("new", "PortName")} {
		r := dataReader(t, c, spec, dataTypes())
		require.ErrorContains(t, r.Write(ctx, "state", 0, []byte{1}), "WriteData")
	}
	r := dataReader(t, c, bitType(9), dataTypes())
	require.Error(t, r.WriteData(ctx, "missing", 0, bitData(1)))
	for _, index := range []int64{-1, -2, 8, math.MaxInt64} {
		require.ErrorContains(t, r.WriteData(ctx, "state", index, bitData(1)), "index")
	}
}

func TestRegisterData_MetadataAndStacks(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	headerStack := func(size int32, name string) *p4configv1.P4DataTypeSpec {
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_HeaderStack{HeaderStack: &p4configv1.P4HeaderStackTypeSpec{Header: &p4configv1.P4NamedType{Name: name}, Size: size}}}
	}
	unionStack := func(size int32, name string) *p4configv1.P4DataTypeSpec {
		return &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_HeaderUnionStack{HeaderUnionStack: &p4configv1.P4HeaderUnionStackTypeSpec{HeaderUnion: &p4configv1.P4NamedType{Name: name}, Size: size}}}
	}
	for _, tc := range []struct {
		name    string
		spec    *p4configv1.P4DataTypeSpec
		value   *p4v1.P4Data
		message string
	}{
		{"header stack length", headerStack(2, "H"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{}}}, "2 entries"},
		{"header stack wrong field", headerStack(0, "H"), bitData(1), "header_stack"},
		{"header stack negative size", headerStack(-1, "H"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{}}}, "entries"},
		{"header stack missing type", headerStack(0, "missing"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{}}}, "unknown"},
		{"header stack nil entry", headerStack(1, "H"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderStack{HeaderStack: &p4v1.P4HeaderStack{Entries: []*p4v1.P4Header{nil}}}}, "entry 0"},
		{"union stack length", unionStack(2, "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{}}}, "2 entries"},
		{"union stack wrong field", unionStack(0, "U"), bitData(1), "header_union_stack"},
		{"union stack negative size", unionStack(-1, "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{}}}, "entries"},
		{"union stack missing type", unionStack(0, "missing"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{}}}, "unknown"},
		{"union stack nil entry", unionStack(1, "U"), &p4v1.P4Data{Data: &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: &p4v1.P4HeaderUnionStack{Entries: []*p4v1.P4HeaderUnion{nil}}}}, "entry 0"},
		{"error missing member", &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Error{Error: &p4configv1.P4ErrorType{}}}, &p4v1.P4Data{Data: &p4v1.P4Data_Error{Error: "Other"}}, "declared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dataReader(t, c, tc.spec, dataTypes())
			require.ErrorContains(t, r.WriteData(ctx, "state", 0, tc.value), tc.message)
		})
	}
	for _, kind := range []string{"struct", "header", "union", "enum", "serializable enum", "new"} {
		r := dataReader(t, c, namedType(kind, "missing"), nil)
		require.ErrorContains(t, r.WriteData(ctx, "state", 0, bitData(0)), "unknown")
	}
	types := dataTypes()
	types.Headers["Variable"] = &p4configv1.P4HeaderTypeSpec{Members: []*p4configv1.P4HeaderTypeSpec_Member{{Name: "payload", TypeSpec: varbitType(32).GetBitstring()}}}
	r := dataReader(t, c, namedType("header", "Variable"), types)
	require.ErrorContains(t, r.WriteData(ctx, "state", 0, &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{IsValid: true, Bitstrings: [][]byte{{0, 1}}}}}), "no explicit bitwidth")
	types.Structs["Record"].Members[0] = nil
	_, err := dataPipeline(namedType("struct", "Record"), types)
	require.ErrorContains(t, err, "missing P4Info")
	types.Structs["Record"].Members[0] = dataTypes().Structs["Record"].Members[0]
	types.NewTypes["Port"].GetTranslatedType().SdnType = nil
	r = dataReader(t, c, namedType("new", "Port"), types)
	require.ErrorContains(t, r.WriteData(ctx, "state", 0, bitData(1)), "missing translation")
	types.NewTypes["Port"].GetTranslatedType().SdnType = &p4configv1.P4NewTypeTranslation_SdnBitwidth{SdnBitwidth: 0}
	r = dataReader(t, c, namedType("new", "Port"), types)
	require.ErrorContains(t, r.WriteData(ctx, "state", 0, bitData(1)), "must be positive")
	h.Mu.Lock()
	require.Empty(t, h.WriteRequests)
	h.Mu.Unlock()
	// An invalid header has no fields to encode, even if its type declares varbit.
	r = dataReader(t, c, namedType("header", "Variable"), types)
	require.NoError(t, r.WriteData(ctx, "state", 0, &p4v1.P4Data{Data: &p4v1.P4Data_Header{Header: &p4v1.P4Header{}}}))
}

func TestRegisterData_CopiesInput(t *testing.T) {
	h := testutil.StartServer(t)
	sent := make(chan *p4v1.WriteRequest, 1)
	resume := make(chan struct{})
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if request, ok := req.(*p4v1.WriteRequest); ok {
			sent <- request
			select {
			case <-resume:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	ctx, c := dataClient(t, h, grpc.WithUnaryInterceptor(interceptor))
	r := dataReader(t, c, tupleType(bitType(9), intType(9)), dataTypes())
	value := tupleData(bitData(0, 1), bitData(255, 255))
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 100, protowire.VarintType), 7)
	value.ProtoReflect().SetUnknown(unknown)
	value.GetTuple().ProtoReflect().SetUnknown(unknown)
	value.GetTuple().Members[0].ProtoReflect().SetUnknown(unknown)
	done := make(chan error, 1)
	go func() { done <- r.WriteData(ctx, "state", 0, value) }()
	var request *p4v1.WriteRequest
	select {
	case request = <-sent:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	value.GetTuple().Members[0].GetBitstring()[1] = 2
	value.GetTuple().Members[1].GetBitstring()[1] = 0
	value.ProtoReflect().GetUnknown()[len(unknown)-1] = 9
	value.GetTuple().Members = append(value.GetTuple().Members, boolData(true))
	close(resume)
	require.NoError(t, <-done)
	data := request.Updates[0].Entity.GetRegisterEntry().Data
	require.Len(t, data.GetTuple().Members, 2)
	require.Equal(t, []byte{1}, data.GetTuple().Members[0].GetBitstring())
	require.Equal(t, []byte{255}, data.GetTuple().Members[1].GetBitstring())
	require.Equal(t, byte(7), data.ProtoReflect().GetUnknown()[len(unknown)-1])
	require.Equal(t, byte(7), data.GetTuple().ProtoReflect().GetUnknown()[len(unknown)-1])
	require.Equal(t, byte(7), data.GetTuple().Members[0].ProtoReflect().GetUnknown()[len(unknown)-1])
}

func TestRegisterData_RecursiveTypes(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	spec := tupleType()
	spec.GetTuple().Members = []*p4configv1.P4DataTypeSpec{spec}
	_, err := dataPipeline(spec, dataTypes())
	require.ErrorContains(t, err, "recursive")
	types := dataTypes()
	types.NewTypes["Loop"] = &p4configv1.P4NewTypeSpec{Representation: &p4configv1.P4NewTypeSpec_OriginalType{OriginalType: namedType("new", "Loop")}}
	r := dataReader(t, c, namedType("new", "Loop"), types)
	require.ErrorContains(t, r.WriteData(ctx, "state", 0, bitData(1)), "recursive")
	cycle := tupleData()
	cycle.GetTuple().Members = []*p4v1.P4Data{cycle}
	r = dataReader(t, c, tupleType(boolType()), dataTypes())
	require.ErrorContains(t, r.WriteData(ctx, "state", 0, cycle), "bool")
}

func TestRegisterData_ConcurrentWrites(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, c := dataClient(t, h)
	r := dataReader(t, c, tupleType(bitType(9), boolType()), dataTypes())
	value := tupleData(bitData(0, 1), boolData(false))
	failures := make(chan error, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int64) { defer wg.Done(); failures <- r.WriteData(ctx, "state", index, value) }(int64(i % 8))
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
