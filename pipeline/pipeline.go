package pipeline

import (
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// Pipeline is the controller-side view of a P4 forwarding pipeline. It wraps
// the parsed P4Info together with the opaque device configuration blob and
// exposes by-name lookups for every first-class object: tables, actions,
// action parameters, counters, direct counters, meters, direct meters,
// registers, digests, and controller packet metadata.
//
// A Pipeline is immutable after construction and safe for concurrent use.
type Pipeline struct {
	info         *p4configv1.P4Info
	deviceConfig []byte

	tablesByName         map[string]*TableDef
	tablesByID           map[uint32]*TableDef
	actionsByName        map[string]*ActionDef
	actionsByID          map[uint32]*ActionDef
	countersByName       map[string]*CounterDef
	countersByID         map[uint32]*CounterDef
	directCountersByName map[string]*DirectCounterDef
	directCountersByID   map[uint32]*DirectCounterDef
	metersByName         map[string]*MeterDef
	metersByID           map[uint32]*MeterDef
	directMetersByName   map[string]*DirectMeterDef
	directMetersByID     map[uint32]*DirectMeterDef
	registersByName      map[string]*RegisterDef
	registersByID        map[uint32]*RegisterDef
	digestsByName        map[string]*DigestDef
	digestsByID          map[uint32]*DigestDef
	ctrlPktByName        map[string]*ControllerPacketMetadataDef
	ctrlPktByID          map[uint32]*ControllerPacketMetadataDef
}

// MatchFieldDef is the controller-facing representation of a single match
// field on a P4 table.
type MatchFieldDef struct {
	ID             uint32
	Name           string
	Bitwidth       int32
	MatchType      p4configv1.MatchField_MatchType
	OtherMatchType string
}

// TableDef describes a P4 table and its match/action structure.
type TableDef struct {
	ID          uint32
	Name        string
	Alias       string
	Size        int64
	Const       bool
	MatchFields []*MatchFieldDef
	ActionRefs  []*ActionRef
	matchByName map[string]*MatchFieldDef
	matchByID   map[uint32]*MatchFieldDef
	raw         *p4configv1.Table
}

// MatchField returns the match field with the given name, or ok=false if no
// such field is declared on the table.
func (t *TableDef) MatchField(name string) (*MatchFieldDef, bool) {
	if t == nil {
		return nil, false
	}
	m, ok := t.matchByName[name]
	return m, ok
}

// MatchFieldByID returns the match field with the given ID.
func (t *TableDef) MatchFieldByID(id uint32) (*MatchFieldDef, bool) {
	if t == nil {
		return nil, false
	}
	m, ok := t.matchByID[id]
	return m, ok
}

// Raw returns the underlying P4Info proto message.
func (t *TableDef) Raw() *p4configv1.Table { return t.raw }

// ActionRef is a reference to an Action from inside a TableDef.
type ActionRef struct {
	ID    uint32
	Scope p4configv1.ActionRef_Scope
}

// ActionDef describes a P4 action.
type ActionDef struct {
	ID          uint32
	Name        string
	Alias       string
	Params      []*ActionParamDef
	paramByName map[string]*ActionParamDef
	paramByID   map[uint32]*ActionParamDef
	raw         *p4configv1.Action
}

// Param returns the action parameter with the given name.
func (a *ActionDef) Param(name string) (*ActionParamDef, bool) {
	if a == nil {
		return nil, false
	}
	p, ok := a.paramByName[name]
	return p, ok
}

// ParamByID returns the action parameter with the given ID.
func (a *ActionDef) ParamByID(id uint32) (*ActionParamDef, bool) {
	if a == nil {
		return nil, false
	}
	p, ok := a.paramByID[id]
	return p, ok
}

// Raw returns the underlying P4Info proto message.
func (a *ActionDef) Raw() *p4configv1.Action { return a.raw }

// ActionParamDef describes a single parameter on an action.
type ActionParamDef struct {
	ID       uint32
	Name     string
	Bitwidth int32
}

// CounterDef describes an indirect counter array.
type CounterDef struct {
	ID   uint32
	Name string
	Unit p4configv1.CounterSpec_Unit
	Size int64
	raw  *p4configv1.Counter
}

// Raw returns the underlying P4Info proto message.
func (c *CounterDef) Raw() *p4configv1.Counter { return c.raw }

// DirectCounterDef describes a direct counter attached to a table.
type DirectCounterDef struct {
	ID              uint32
	Name            string
	Unit            p4configv1.CounterSpec_Unit
	DirectTableID   uint32
	DirectTableName string
	raw             *p4configv1.DirectCounter
}

// Raw returns the underlying P4Info proto message.
func (c *DirectCounterDef) Raw() *p4configv1.DirectCounter { return c.raw }

// MeterDef describes an indirect meter array.
type MeterDef struct {
	ID   uint32
	Name string
	Unit p4configv1.MeterSpec_Unit
	Size int64
	raw  *p4configv1.Meter
}

// Raw returns the underlying P4Info proto message.
func (m *MeterDef) Raw() *p4configv1.Meter { return m.raw }

// DirectMeterDef describes a direct meter attached to a table.
type DirectMeterDef struct {
	ID              uint32
	Name            string
	Unit            p4configv1.MeterSpec_Unit
	DirectTableID   uint32
	DirectTableName string
	raw             *p4configv1.DirectMeter
}

// Raw returns the underlying P4Info proto message.
func (m *DirectMeterDef) Raw() *p4configv1.DirectMeter { return m.raw }

// RegisterDef describes a P4 register array.
type RegisterDef struct {
	ID   uint32
	Name string
	Size int32
	raw  *p4configv1.Register
}

// Raw returns the underlying P4Info proto message.
func (r *RegisterDef) Raw() *p4configv1.Register { return r.raw }

// DigestDef describes a P4 digest declaration.
type DigestDef struct {
	ID   uint32
	Name string
	raw  *p4configv1.Digest
}

// Raw returns the underlying P4Info proto message.
func (d *DigestDef) Raw() *p4configv1.Digest { return d.raw }

// ControllerPacketMetadataDef describes either PacketIn or PacketOut
// controller packet metadata.
type ControllerPacketMetadataDef struct {
	ID       uint32
	Name     string
	Metadata []*PacketMetadataField
	byName   map[string]*PacketMetadataField
	byID     map[uint32]*PacketMetadataField
	raw      *p4configv1.ControllerPacketMetadata
}

// Field returns the metadata field with the given name.
func (c *ControllerPacketMetadataDef) Field(name string) (*PacketMetadataField, bool) {
	if c == nil {
		return nil, false
	}
	f, ok := c.byName[name]
	return f, ok
}

// FieldByID returns the metadata field with the given ID.
func (c *ControllerPacketMetadataDef) FieldByID(id uint32) (*PacketMetadataField, bool) {
	if c == nil {
		return nil, false
	}
	f, ok := c.byID[id]
	return f, ok
}

// Raw returns the underlying P4Info proto message.
func (c *ControllerPacketMetadataDef) Raw() *p4configv1.ControllerPacketMetadata { return c.raw }

// PacketMetadataField is a single field inside a controller packet metadata
// header.
type PacketMetadataField struct {
	ID       uint32
	Name     string
	Bitwidth int32
}

// New builds a Pipeline from a pre-parsed P4Info message and an optional
// opaque device configuration blob. The device configuration is passed
// through to SetForwardingPipelineConfig verbatim.
func New(info *p4configv1.P4Info, deviceConfig []byte) (*Pipeline, error) {
	if info == nil {
		return nil, fmt.Errorf("pipeline.New: nil P4Info")
	}
	p := &Pipeline{
		info:                 info,
		deviceConfig:         append([]byte(nil), deviceConfig...),
		tablesByName:         map[string]*TableDef{},
		tablesByID:           map[uint32]*TableDef{},
		actionsByName:        map[string]*ActionDef{},
		actionsByID:          map[uint32]*ActionDef{},
		countersByName:       map[string]*CounterDef{},
		countersByID:         map[uint32]*CounterDef{},
		directCountersByName: map[string]*DirectCounterDef{},
		directCountersByID:   map[uint32]*DirectCounterDef{},
		metersByName:         map[string]*MeterDef{},
		metersByID:           map[uint32]*MeterDef{},
		directMetersByName:   map[string]*DirectMeterDef{},
		directMetersByID:     map[uint32]*DirectMeterDef{},
		registersByName:      map[string]*RegisterDef{},
		registersByID:        map[uint32]*RegisterDef{},
		digestsByName:        map[string]*DigestDef{},
		digestsByID:          map[uint32]*DigestDef{},
		ctrlPktByName:        map[string]*ControllerPacketMetadataDef{},
		ctrlPktByID:          map[uint32]*ControllerPacketMetadataDef{},
	}
	if err := p.index(); err != nil {
		return nil, err
	}
	return p, nil
}

// Load parses a binary-encoded P4Info (proto wire format) together with an
// optional device configuration blob.
func Load(p4infoBytes, deviceConfig []byte) (*Pipeline, error) {
	info := &p4configv1.P4Info{}
	if err := proto.Unmarshal(p4infoBytes, info); err != nil {
		return nil, fmt.Errorf("pipeline.Load: unmarshal P4Info: %w", err)
	}
	return New(info, deviceConfig)
}

// LoadText parses a text-format P4Info (as produced by `p4c --p4runtime-files
// ... .p4info.txt`) together with an optional device configuration blob.
func LoadText(p4infoText []byte, deviceConfig []byte) (*Pipeline, error) {
	info := &p4configv1.P4Info{}
	if err := prototext.Unmarshal(p4infoText, info); err != nil {
		return nil, fmt.Errorf("pipeline.LoadText: unmarshal P4Info: %w", err)
	}
	return New(info, deviceConfig)
}

// Info returns the underlying P4Info proto. Callers must treat the result as
// read-only.
func (p *Pipeline) Info() *p4configv1.P4Info { return p.info }

// DeviceConfig returns a copy of the opaque device configuration blob.
func (p *Pipeline) DeviceConfig() []byte {
	if len(p.deviceConfig) == 0 {
		return nil
	}
	out := make([]byte, len(p.deviceConfig))
	copy(out, p.deviceConfig)
	return out
}

// Table returns the table with the given fully qualified name.
func (p *Pipeline) Table(name string) (*TableDef, bool) {
	if p == nil {
		return nil, false
	}
	t, ok := p.tablesByName[name]
	return t, ok
}

// TableByID returns the table with the given P4Info ID.
func (p *Pipeline) TableByID(id uint32) (*TableDef, bool) {
	if p == nil {
		return nil, false
	}
	t, ok := p.tablesByID[id]
	return t, ok
}

// Tables returns every table in the pipeline, in P4Info order.
func (p *Pipeline) Tables() []*TableDef {
	out := make([]*TableDef, 0, len(p.tablesByName))
	for _, t := range p.info.GetTables() {
		if td, ok := p.tablesByID[t.GetPreamble().GetId()]; ok {
			out = append(out, td)
		}
	}
	return out
}

// Action returns the action with the given fully qualified name.
func (p *Pipeline) Action(name string) (*ActionDef, bool) {
	if p == nil {
		return nil, false
	}
	a, ok := p.actionsByName[name]
	return a, ok
}

// ActionByID returns the action with the given P4Info ID.
func (p *Pipeline) ActionByID(id uint32) (*ActionDef, bool) {
	if p == nil {
		return nil, false
	}
	a, ok := p.actionsByID[id]
	return a, ok
}

// Counter returns the indirect counter with the given fully qualified name.
func (p *Pipeline) Counter(name string) (*CounterDef, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.countersByName[name]
	return c, ok
}

// CounterByID returns the indirect counter with the given ID.
func (p *Pipeline) CounterByID(id uint32) (*CounterDef, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.countersByID[id]
	return c, ok
}

// DirectCounter returns the direct counter with the given name.
func (p *Pipeline) DirectCounter(name string) (*DirectCounterDef, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.directCountersByName[name]
	return c, ok
}

// Meter returns the indirect meter with the given name.
func (p *Pipeline) Meter(name string) (*MeterDef, bool) {
	if p == nil {
		return nil, false
	}
	m, ok := p.metersByName[name]
	return m, ok
}

// DirectMeter returns the direct meter with the given name.
func (p *Pipeline) DirectMeter(name string) (*DirectMeterDef, bool) {
	if p == nil {
		return nil, false
	}
	m, ok := p.directMetersByName[name]
	return m, ok
}

// Register returns the register array with the given name.
func (p *Pipeline) Register(name string) (*RegisterDef, bool) {
	if p == nil {
		return nil, false
	}
	r, ok := p.registersByName[name]
	return r, ok
}

// Digest returns the digest with the given name.
func (p *Pipeline) Digest(name string) (*DigestDef, bool) {
	if p == nil {
		return nil, false
	}
	d, ok := p.digestsByName[name]
	return d, ok
}

// PacketMetadata returns the controller packet metadata header with the
// given name (typically "packet_in" or "packet_out").
func (p *Pipeline) PacketMetadata(name string) (*ControllerPacketMetadataDef, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.ctrlPktByName[name]
	return c, ok
}

// PacketMetadataByID returns the controller packet metadata header with the
// given ID.
func (p *Pipeline) PacketMetadataByID(id uint32) (*ControllerPacketMetadataDef, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.ctrlPktByID[id]
	return c, ok
}

func (p *Pipeline) index() error {
	for _, raw := range p.info.GetActions() {
		a := &ActionDef{
			ID:          raw.GetPreamble().GetId(),
			Name:        raw.GetPreamble().GetName(),
			Alias:       raw.GetPreamble().GetAlias(),
			paramByName: map[string]*ActionParamDef{},
			paramByID:   map[uint32]*ActionParamDef{},
			raw:         raw,
		}
		for _, pr := range raw.GetParams() {
			pd := &ActionParamDef{
				ID:       pr.GetId(),
				Name:     pr.GetName(),
				Bitwidth: pr.GetBitwidth(),
			}
			a.Params = append(a.Params, pd)
			a.paramByName[pd.Name] = pd
			a.paramByID[pd.ID] = pd
		}
		p.actionsByName[a.Name] = a
		if a.Alias != "" {
			p.actionsByName[a.Alias] = a
		}
		p.actionsByID[a.ID] = a
	}

	for _, raw := range p.info.GetTables() {
		t := &TableDef{
			ID:          raw.GetPreamble().GetId(),
			Name:        raw.GetPreamble().GetName(),
			Alias:       raw.GetPreamble().GetAlias(),
			Size:        raw.GetSize(),
			Const:       raw.GetIsConstTable(),
			matchByName: map[string]*MatchFieldDef{},
			matchByID:   map[uint32]*MatchFieldDef{},
			raw:         raw,
		}
		for _, mf := range raw.GetMatchFields() {
			md := &MatchFieldDef{
				ID:             mf.GetId(),
				Name:           mf.GetName(),
				Bitwidth:       mf.GetBitwidth(),
				MatchType:      mf.GetMatchType(),
				OtherMatchType: mf.GetOtherMatchType(),
			}
			t.MatchFields = append(t.MatchFields, md)
			t.matchByName[md.Name] = md
			t.matchByID[md.ID] = md
		}
		for _, ar := range raw.GetActionRefs() {
			t.ActionRefs = append(t.ActionRefs, &ActionRef{
				ID:    ar.GetId(),
				Scope: ar.GetScope(),
			})
		}
		p.tablesByName[t.Name] = t
		if t.Alias != "" {
			p.tablesByName[t.Alias] = t
		}
		p.tablesByID[t.ID] = t
	}

	for _, raw := range p.info.GetCounters() {
		c := &CounterDef{
			ID:   raw.GetPreamble().GetId(),
			Name: raw.GetPreamble().GetName(),
			Unit: raw.GetSpec().GetUnit(),
			Size: raw.GetSize(),
			raw:  raw,
		}
		p.countersByName[c.Name] = c
		if alias := raw.GetPreamble().GetAlias(); alias != "" {
			p.countersByName[alias] = c
		}
		p.countersByID[c.ID] = c
	}

	for _, raw := range p.info.GetDirectCounters() {
		dc := &DirectCounterDef{
			ID:            raw.GetPreamble().GetId(),
			Name:          raw.GetPreamble().GetName(),
			Unit:          raw.GetSpec().GetUnit(),
			DirectTableID: raw.GetDirectTableId(),
			raw:           raw,
		}
		if t, ok := p.tablesByID[dc.DirectTableID]; ok {
			dc.DirectTableName = t.Name
		}
		p.directCountersByName[dc.Name] = dc
		p.directCountersByID[dc.ID] = dc
	}

	for _, raw := range p.info.GetMeters() {
		m := &MeterDef{
			ID:   raw.GetPreamble().GetId(),
			Name: raw.GetPreamble().GetName(),
			Unit: raw.GetSpec().GetUnit(),
			Size: raw.GetSize(),
			raw:  raw,
		}
		p.metersByName[m.Name] = m
		if alias := raw.GetPreamble().GetAlias(); alias != "" {
			p.metersByName[alias] = m
		}
		p.metersByID[m.ID] = m
	}

	for _, raw := range p.info.GetDirectMeters() {
		dm := &DirectMeterDef{
			ID:            raw.GetPreamble().GetId(),
			Name:          raw.GetPreamble().GetName(),
			Unit:          raw.GetSpec().GetUnit(),
			DirectTableID: raw.GetDirectTableId(),
			raw:           raw,
		}
		if t, ok := p.tablesByID[dm.DirectTableID]; ok {
			dm.DirectTableName = t.Name
		}
		p.directMetersByName[dm.Name] = dm
		p.directMetersByID[dm.ID] = dm
	}

	for _, raw := range p.info.GetRegisters() {
		r := &RegisterDef{
			ID:   raw.GetPreamble().GetId(),
			Name: raw.GetPreamble().GetName(),
			Size: raw.GetSize(),
			raw:  raw,
		}
		p.registersByName[r.Name] = r
		if alias := raw.GetPreamble().GetAlias(); alias != "" {
			p.registersByName[alias] = r
		}
		p.registersByID[r.ID] = r
	}

	for _, raw := range p.info.GetDigests() {
		d := &DigestDef{
			ID:   raw.GetPreamble().GetId(),
			Name: raw.GetPreamble().GetName(),
			raw:  raw,
		}
		p.digestsByName[d.Name] = d
		if alias := raw.GetPreamble().GetAlias(); alias != "" {
			p.digestsByName[alias] = d
		}
		p.digestsByID[d.ID] = d
	}

	for _, raw := range p.info.GetControllerPacketMetadata() {
		c := &ControllerPacketMetadataDef{
			ID:     raw.GetPreamble().GetId(),
			Name:   raw.GetPreamble().GetName(),
			byName: map[string]*PacketMetadataField{},
			byID:   map[uint32]*PacketMetadataField{},
			raw:    raw,
		}
		for _, mf := range raw.GetMetadata() {
			f := &PacketMetadataField{
				ID:       mf.GetId(),
				Name:     mf.GetName(),
				Bitwidth: mf.GetBitwidth(),
			}
			c.Metadata = append(c.Metadata, f)
			c.byName[f.Name] = f
			c.byID[f.ID] = f
		}
		p.ctrlPktByName[c.Name] = c
		p.ctrlPktByID[c.ID] = c
	}
	return nil
}
