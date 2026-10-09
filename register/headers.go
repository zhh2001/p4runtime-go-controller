package register

import (
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
)

func (n *dataNormalizer) header(name string, value *p4v1.P4Header) (*p4v1.P4Header, error) {
	decl := n.types.GetHeaders()[name]
	if name == "" || decl == nil {
		return nil, fmt.Errorf("unknown header type %q", name)
	}
	if value == nil {
		return nil, fmt.Errorf("expected P4Data.header for %q", name)
	}
	if !value.IsValid && len(value.Bitstrings) != 0 {
		return nil, fmt.Errorf("invalid header %q must have no fields", name)
	}
	out := &p4v1.P4Header{IsValid: value.IsValid}
	if value.IsValid {
		if len(value.Bitstrings) != len(decl.Members) {
			return nil, fmt.Errorf("header %q requires %d fields", name, len(decl.Members))
		}
		out.Bitstrings = make([][]byte, len(decl.Members))
		for i, member := range decl.Members {
			spec := member.GetTypeSpec()
			if spec != nil {
				if _, ok := spec.TypeSpec.(*p4configv1.P4BitstringLikeTypeSpec_Varbit); ok {
					return nil, fmt.Errorf("header %q field %d: varbit has no explicit bitwidth in P4Header", name, i)
				}
			}
			data, err := n.bitstring(spec, &p4v1.P4Data{Data: &p4v1.P4Data_Bitstring{Bitstring: value.Bitstrings[i]}}, true)
			if err != nil {
				return nil, fmt.Errorf("header %q field %d: %w", name, i, err)
			}
			out.Bitstrings[i] = data.GetBitstring()
		}
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) headerUnion(name string, value *p4v1.P4HeaderUnion) (*p4v1.P4HeaderUnion, error) {
	decl := n.types.GetHeaderUnions()[name]
	if name == "" || decl == nil {
		return nil, fmt.Errorf("unknown header union type %q", name)
	}
	if value == nil {
		return nil, fmt.Errorf("expected P4Data.header_union for %q", name)
	}
	out := &p4v1.P4HeaderUnion{ValidHeaderName: value.ValidHeaderName}
	if value.ValidHeaderName == "" {
		if value.ValidHeader != nil {
			return nil, fmt.Errorf("invalid header union %q must have no valid_header", name)
		}
	} else {
		var headerName string
		for _, member := range decl.Members {
			if member.GetName() == value.ValidHeaderName {
				headerName = member.GetHeader().GetName()
				break
			}
		}
		if headerName == "" || !value.GetValidHeader().GetIsValid() {
			return nil, fmt.Errorf("header union %q requires a valid declared header %q", name, value.ValidHeaderName)
		}
		header, err := n.header(headerName, value.ValidHeader)
		if err != nil {
			return nil, err
		}
		out.ValidHeader = header
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) headerStack(spec *p4configv1.P4HeaderStackTypeSpec, value *p4v1.P4HeaderStack) (*p4v1.P4HeaderStack, error) {
	if spec == nil || value == nil || spec.Size < 0 || int64(len(value.Entries)) != int64(spec.Size) {
		return nil, fmt.Errorf("expected P4Data.header_stack with %d entries", spec.GetSize())
	}
	name := spec.GetHeader().GetName()
	if name == "" || n.types.GetHeaders()[name] == nil {
		return nil, fmt.Errorf("unknown header type %q", name)
	}
	out := &p4v1.P4HeaderStack{Entries: make([]*p4v1.P4Header, len(value.Entries))}
	for i, entry := range value.Entries {
		header, err := n.header(name, entry)
		if err != nil {
			return nil, fmt.Errorf("header stack entry %d: %w", i, err)
		}
		out.Entries[i] = header
	}
	copyUnknown(value, out)
	return out, nil
}

func (n *dataNormalizer) headerUnionStack(spec *p4configv1.P4HeaderUnionStackTypeSpec, value *p4v1.P4HeaderUnionStack) (*p4v1.P4HeaderUnionStack, error) {
	if spec == nil || value == nil || spec.Size < 0 || int64(len(value.Entries)) != int64(spec.Size) {
		return nil, fmt.Errorf("expected P4Data.header_union_stack with %d entries", spec.GetSize())
	}
	name := spec.GetHeaderUnion().GetName()
	if name == "" || n.types.GetHeaderUnions()[name] == nil {
		return nil, fmt.Errorf("unknown header union type %q", name)
	}
	out := &p4v1.P4HeaderUnionStack{Entries: make([]*p4v1.P4HeaderUnion, len(value.Entries))}
	for i, entry := range value.Entries {
		union, err := n.headerUnion(name, entry)
		if err != nil {
			return nil, fmt.Errorf("header union stack entry %d: %w", i, err)
		}
		out.Entries[i] = union
	}
	copyUnknown(value, out)
	return out, nil
}
