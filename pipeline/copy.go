package pipeline

// Raw messages remain private and immutable. Raw returns a deep copy, so
// definition copies can safely retain their private raw reference.
func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyValues[T any](values []*T) []*T {
	if values == nil {
		return nil
	}
	out := make([]*T, len(values))
	for i, value := range values {
		out[i] = copyValue(value)
	}
	return out
}

func (t *TableDef) clone() *TableDef {
	out := copyValue(t)
	if out == nil {
		return nil
	}
	out.MatchFields = copyValues(t.MatchFields)
	out.ActionRefs = copyValues(t.ActionRefs)
	out.matchByName = make(map[string]*MatchFieldDef, len(t.matchByName))
	out.matchByID = make(map[uint32]*MatchFieldDef, len(t.matchByID))
	for _, field := range out.MatchFields {
		out.matchByName[field.Name] = field
		out.matchByID[field.ID] = field
	}
	return out
}

func (a *ActionDef) clone() *ActionDef {
	out := copyValue(a)
	if out == nil {
		return nil
	}
	out.Params = copyValues(a.Params)
	out.paramByName = make(map[string]*ActionParamDef, len(a.paramByName))
	out.paramByID = make(map[uint32]*ActionParamDef, len(a.paramByID))
	for _, param := range out.Params {
		out.paramByName[param.Name] = param
		out.paramByID[param.ID] = param
	}
	return out
}

func (c *ControllerPacketMetadataDef) clone() *ControllerPacketMetadataDef {
	out := copyValue(c)
	if out == nil {
		return nil
	}
	out.Metadata = copyValues(c.Metadata)
	out.byName = make(map[string]*PacketMetadataField, len(c.byName))
	out.byID = make(map[uint32]*PacketMetadataField, len(c.byID))
	for _, field := range out.Metadata {
		out.byName[field.Name] = field
		out.byID[field.ID] = field
	}
	return out
}
