package register

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/proto"
)

type dataNormalizer struct {
	types  *p4configv1.P4TypeInfo
	active map[*p4configv1.P4DataTypeSpec]bool
}

// Generated oneofs hold pointers, including when callers assign typed nils.
func missingOneof(value any) bool {
	return value == nil || reflect.ValueOf(value).IsNil()
}

func copyUnknown(from, to proto.Message) {
	to.ProtoReflect().SetUnknown(bytes.Clone(from.ProtoReflect().GetUnknown()))
}

func (n *dataNormalizer) normalize(spec *p4configv1.P4DataTypeSpec, value *p4v1.P4Data, integerOnly bool) (*p4v1.P4Data, error) {
	if spec == nil || missingOneof(spec.TypeSpec) {
		return nil, fmt.Errorf("missing P4Info type specification")
	}
	if value == nil || missingOneof(value.Data) {
		return nil, fmt.Errorf("missing P4Data value")
	}
	if n.active[spec] {
		return nil, fmt.Errorf("recursive P4Info type specification")
	}
	n.active[spec] = true
	defer delete(n.active, spec)
	if _, ok := spec.TypeSpec.(*p4configv1.P4DataTypeSpec_NewType); ok {
		return n.newType(spec.GetNewType().GetName(), value, integerOnly)
	}
	if integerOnly {
		if _, ok := spec.TypeSpec.(*p4configv1.P4DataTypeSpec_Bitstring); !ok {
			return nil, fmt.Errorf("Write requires bit<W> or int<W>, use WriteData for this type")
		}
	}
	out := &p4v1.P4Data{}
	switch spec.TypeSpec.(type) {
	case *p4configv1.P4DataTypeSpec_Bitstring:
		return n.bitstring(spec.GetBitstring(), value, integerOnly)
	case *p4configv1.P4DataTypeSpec_Bool:
		if spec.GetBool() == nil {
			return nil, fmt.Errorf("missing bool type declaration")
		}
		if _, ok := value.Data.(*p4v1.P4Data_Bool); !ok {
			return nil, fmt.Errorf("expected P4Data.bool")
		}
		out.Data = &p4v1.P4Data_Bool{Bool: value.GetBool()}
	case *p4configv1.P4DataTypeSpec_Tuple:
		if spec.GetTuple() == nil {
			return nil, fmt.Errorf("missing tuple type declaration")
		}
		members, err := n.members(spec.GetTuple().Members, value.GetTuple())
		if err != nil {
			return nil, fmt.Errorf("tuple: %w", err)
		}
		out.Data = &p4v1.P4Data_Tuple{Tuple: members}
	case *p4configv1.P4DataTypeSpec_Struct:
		name := spec.GetStruct().GetName()
		decl := n.types.GetStructs()[name]
		if name == "" || decl == nil {
			return nil, fmt.Errorf("unknown struct type %q", name)
		}
		members := make([]*p4configv1.P4DataTypeSpec, len(decl.Members))
		for i, member := range decl.Members {
			members[i] = member.GetTypeSpec()
		}
		data, err := n.members(members, value.GetStruct())
		if err != nil {
			return nil, fmt.Errorf("struct %q: %w", name, err)
		}
		out.Data = &p4v1.P4Data_Struct{Struct: data}
	case *p4configv1.P4DataTypeSpec_Header:
		header, err := n.header(spec.GetHeader().GetName(), value.GetHeader())
		if err != nil {
			return nil, err
		}
		out.Data = &p4v1.P4Data_Header{Header: header}
	case *p4configv1.P4DataTypeSpec_HeaderUnion:
		union, err := n.headerUnion(spec.GetHeaderUnion().GetName(), value.GetHeaderUnion())
		if err != nil {
			return nil, err
		}
		out.Data = &p4v1.P4Data_HeaderUnion{HeaderUnion: union}
	case *p4configv1.P4DataTypeSpec_HeaderStack:
		stack, err := n.headerStack(spec.GetHeaderStack(), value.GetHeaderStack())
		if err != nil {
			return nil, err
		}
		out.Data = &p4v1.P4Data_HeaderStack{HeaderStack: stack}
	case *p4configv1.P4DataTypeSpec_HeaderUnionStack:
		stack, err := n.headerUnionStack(spec.GetHeaderUnionStack(), value.GetHeaderUnionStack())
		if err != nil {
			return nil, err
		}
		out.Data = &p4v1.P4Data_HeaderUnionStack{HeaderUnionStack: stack}
	case *p4configv1.P4DataTypeSpec_Enum:
		name := spec.GetEnum().GetName()
		decl := n.types.GetEnums()[name]
		if name == "" || decl == nil {
			return nil, fmt.Errorf("unknown enum type %q", name)
		}
		v, ok := value.Data.(*p4v1.P4Data_Enum)
		if !ok || v.Enum == "" || !slices.ContainsFunc(decl.Members, func(m *p4configv1.P4EnumTypeSpec_Member) bool { return m.GetName() == v.Enum }) {
			return nil, fmt.Errorf("expected a declared P4Data.enum member of %q", name)
		}
		out.Data = &p4v1.P4Data_Enum{Enum: v.Enum}
	case *p4configv1.P4DataTypeSpec_Error:
		v, ok := value.Data.(*p4v1.P4Data_Error)
		if spec.GetError() == nil || n.types.GetError() == nil || !ok || v.Error == "" || !slices.Contains(n.types.GetError().Members, v.Error) {
			return nil, fmt.Errorf("expected a declared P4Data.error member")
		}
		out.Data = &p4v1.P4Data_Error{Error: v.Error}
	case *p4configv1.P4DataTypeSpec_SerializableEnum:
		name := spec.GetSerializableEnum().GetName()
		decl := n.types.GetSerializableEnums()[name]
		if name == "" || decl == nil || decl.UnderlyingType == nil {
			return nil, fmt.Errorf("unknown serializable enum type %q", name)
		}
		v, ok := value.Data.(*p4v1.P4Data_EnumValue)
		if !ok {
			return nil, fmt.Errorf("expected P4Data.enum_value for %q", name)
		}
		encoded, err := unsignedBytes(v.EnumValue, decl.UnderlyingType.Bitwidth)
		if err != nil {
			return nil, fmt.Errorf("enum %q: %w", name, err)
		}
		out.Data = &p4v1.P4Data_EnumValue{EnumValue: encoded}
	default:
		return nil, fmt.Errorf("unsupported P4Info type specification")
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) bitstring(spec *p4configv1.P4BitstringLikeTypeSpec, value *p4v1.P4Data, integerOnly bool) (*p4v1.P4Data, error) {
	if spec == nil || missingOneof(spec.TypeSpec) {
		return nil, fmt.Errorf("missing bitstring type declaration")
	}
	out := &p4v1.P4Data{}
	if _, ok := spec.TypeSpec.(*p4configv1.P4BitstringLikeTypeSpec_Varbit); ok {
		if integerOnly {
			return nil, fmt.Errorf("Write requires bit<W> or int<W>, use WriteData for varbit")
		}
		v := value.GetVarbit()
		decl := spec.GetVarbit()
		if v == nil || decl == nil || v.Bitwidth < 0 || v.Bitwidth > decl.MaxBitwidth {
			return nil, fmt.Errorf("expected P4Data.varbit with bitwidth in [0, %d]", decl.GetMaxBitwidth())
		}
		length := (int64(v.Bitwidth) + 7) / 8
		if int64(len(v.Bitstring)) != length {
			return nil, fmt.Errorf("varbit bitwidth %d requires %d bytes", v.Bitwidth, length)
		}
		_, err := unsignedBytes(v.Bitstring, v.Bitwidth)
		if err != nil {
			return nil, fmt.Errorf("varbit: %w", err)
		}
		data := &p4v1.P4Varbit{Bitwidth: v.Bitwidth, Bitstring: bytes.Clone(v.Bitstring)}
		copyUnknown(v, data)
		out.Data = &p4v1.P4Data_Varbit{Varbit: data}
	} else {
		v, ok := value.Data.(*p4v1.P4Data_Bitstring)
		if !ok {
			return nil, fmt.Errorf("expected P4Data.bitstring")
		}
		var encoded []byte
		var err error
		switch spec.TypeSpec.(type) {
		case *p4configv1.P4BitstringLikeTypeSpec_Bit:
			if spec.GetBit() == nil {
				return nil, fmt.Errorf("missing bit type declaration")
			}
			encoded, err = unsignedBytes(v.Bitstring, spec.GetBit().Bitwidth)
		case *p4configv1.P4BitstringLikeTypeSpec_Int:
			if spec.GetInt() == nil {
				return nil, fmt.Errorf("missing int type declaration")
			}
			encoded, err = signedBytes(v.Bitstring, spec.GetInt().Bitwidth)
		default:
			return nil, fmt.Errorf("unsupported bitstring type declaration")
		}
		if err != nil {
			return nil, err
		}
		out.Data = &p4v1.P4Data_Bitstring{Bitstring: encoded}
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) members(specs []*p4configv1.P4DataTypeSpec, value *p4v1.P4StructLike) (*p4v1.P4StructLike, error) {
	if value == nil || len(value.Members) != len(specs) {
		return nil, fmt.Errorf("expected %d members", len(specs))
	}
	out := &p4v1.P4StructLike{Members: make([]*p4v1.P4Data, len(specs))}
	for i, spec := range specs {
		data, err := n.normalize(spec, value.Members[i], false)
		if err != nil {
			return nil, fmt.Errorf("member %d: %w", i, err)
		}
		out.Members[i] = data
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) newType(name string, value *p4v1.P4Data, integerOnly bool) (*p4v1.P4Data, error) {
	decl := n.types.GetNewTypes()[name]
	if name == "" || decl == nil || missingOneof(decl.Representation) {
		return nil, fmt.Errorf("unknown new type %q", name)
	}
	if _, ok := decl.Representation.(*p4configv1.P4NewTypeSpec_OriginalType); ok {
		return n.normalize(decl.GetOriginalType(), value, integerOnly)
	}
	translated := decl.GetTranslatedType()
	if translated == nil || missingOneof(translated.SdnType) {
		return nil, fmt.Errorf("missing translation for type %q", name)
	}
	v, ok := value.Data.(*p4v1.P4Data_Bitstring)
	if !ok {
		return nil, fmt.Errorf("expected P4Data.bitstring for translated type %q", name)
	}
	var encoded []byte
	if _, ok := translated.SdnType.(*p4configv1.P4NewTypeTranslation_SdnString_); ok {
		if translated.GetSdnString() == nil || integerOnly {
			return nil, fmt.Errorf("use WriteData for translated string type %q", name)
		}
		encoded = bytes.Clone(v.Bitstring)
	} else {
		width := translated.GetSdnBitwidth()
		if width <= 0 {
			return nil, fmt.Errorf("translated bitwidth %d must be positive", width)
		}
		var err error
		encoded, err = unsignedBytes(v.Bitstring, width)
		if err != nil {
			return nil, fmt.Errorf("translated type %q: %w", name, err)
		}
	}
	out := &p4v1.P4Data{Data: &p4v1.P4Data_Bitstring{Bitstring: encoded}}
	copyUnknown(value, out)
	return out, nil
}
