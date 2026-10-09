package pipeline_test

import (
	"fmt"
	"sync"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestPipeline_ConstructorOwnsInputs(t *testing.T) {
	info := sampleInfo()
	want := proto.Clone(info)
	config := []byte{1, 2, 3}
	p, err := pipeline.New(info, config)
	require.NoError(t, err)
	info.Tables[0].MatchFields[0].Bitwidth = 1
	info.Tables[0].Preamble.Id = 999
	info.Actions[0].Params[0].Bitwidth = 1
	info.Counters[0].Size = 1
	info.PkgInfo.Arch = "changed"
	info.Tables = nil
	config[0] = 99
	require.True(t, proto.Equal(want, p.Info()), "caller changes altered pipeline P4Info")
	require.Equal(t, []byte{1, 2, 3}, p.DeviceConfig())
	tables := p.Tables()
	require.Len(t, tables, 1)
	require.EqualValues(t, 100, tables[0].ID)
	require.EqualValues(t, 48, tables[0].MatchFields[0].Bitwidth)
	definition, ok := p.TableByID(100)
	require.True(t, ok)
	require.EqualValues(t, 100, definition.Raw().GetPreamble().GetId())
}

func TestPipeline_InfoOwnsReturnedMessage(t *testing.T) {
	p, err := pipeline.New(sampleInfo(), nil)
	require.NoError(t, err)
	want := proto.Clone(p.Info())
	info := p.Info()
	info.Actions[0].Params[0].Bitwidth = 1
	info.Tables[0].MatchFields[0].Bitwidth = 1
	proto.Reset(info)
	require.True(t, proto.Equal(want, p.Info()))
	tables := p.Tables()
	require.Len(t, tables, 1)
	require.EqualValues(t, 48, tables[0].MatchFields[0].Bitwidth)
}

func checkDefinitionCopy[T any](t *testing.T, get func(*pipeline.Pipeline) *T, change func(*T)) {
	t.Helper()
	p, err := pipeline.New(sampleInfo(), nil)
	require.NoError(t, err)
	reference, err := pipeline.New(sampleInfo(), nil)
	require.NoError(t, err)
	want := get(reference)
	got := get(p)
	require.NotNil(t, got)
	change(got)
	fresh := get(p)
	require.NotSame(t, got, fresh)
	require.Equal(t, want, fresh)
}

func TestPipeline_DefinitionCopies(t *testing.T) {
	for name, get := range map[string]func(*pipeline.Pipeline) *pipeline.TableDef{
		"name":  func(p *pipeline.Pipeline) *pipeline.TableDef { d, _ := p.Table("ingress.t_l2"); return d },
		"alias": func(p *pipeline.Pipeline) *pipeline.TableDef { d, _ := p.Table("t_l2"); return d },
		"ID":    func(p *pipeline.Pipeline) *pipeline.TableDef { d, _ := p.TableByID(100); return d },
		"list":  func(p *pipeline.Pipeline) *pipeline.TableDef { return p.Tables()[0] },
	} {
		t.Run("table/"+name, func(t *testing.T) {
			checkDefinitionCopy(t, get, func(d *pipeline.TableDef) {
				d.ID, d.Name, d.Alias, d.Size, d.Const = 999, "changed", "changed", 1, true
				field, ok := d.MatchField("hdr.eth.dst")
				require.True(t, ok)
				require.Same(t, d.MatchFields[0], field)
				byID, ok := d.MatchFieldByID(1)
				require.True(t, ok)
				require.Same(t, field, byID)
				*field = pipeline.MatchFieldDef{ID: 9, Name: "changed", Bitwidth: 1, MatchType: p4configv1.MatchField_OPTIONAL, OtherMatchType: "changed"}
				d.ActionRefs[0].ID = 999
				d.ActionRefs[0].Scope = p4configv1.ActionRef_DEFAULT_ONLY
				d.MatchFields[1] = nil
				d.ActionRefs = append(d.ActionRefs, &pipeline.ActionRef{ID: 888})
			})
		})
	}
	for name, get := range map[string]func(*pipeline.Pipeline) *pipeline.ActionDef{
		"name":  func(p *pipeline.Pipeline) *pipeline.ActionDef { d, _ := p.Action("ingress.forward"); return d },
		"alias": func(p *pipeline.Pipeline) *pipeline.ActionDef { d, _ := p.Action("forward"); return d },
		"ID":    func(p *pipeline.Pipeline) *pipeline.ActionDef { d, _ := p.ActionByID(200); return d },
	} {
		t.Run("action/"+name, func(t *testing.T) {
			checkDefinitionCopy(t, get, func(d *pipeline.ActionDef) {
				d.ID, d.Name, d.Alias = 999, "changed", "changed"
				param, ok := d.Param("port")
				require.True(t, ok)
				require.Same(t, d.Params[0], param)
				byID, ok := d.ParamByID(1)
				require.True(t, ok)
				require.Same(t, param, byID)
				*param = pipeline.ActionParamDef{ID: 9, Name: "changed", Bitwidth: 1}
				d.Params[0] = nil
			})
		})
	}
	for name, get := range map[string]func(*pipeline.Pipeline) *pipeline.CounterDef{
		"name":  func(p *pipeline.Pipeline) *pipeline.CounterDef { d, _ := p.Counter("ingress.pkt_counter"); return d },
		"alias": func(p *pipeline.Pipeline) *pipeline.CounterDef { d, _ := p.Counter("pkt_counter"); return d },
		"ID":    func(p *pipeline.Pipeline) *pipeline.CounterDef { d, _ := p.CounterByID(300); return d },
	} {
		t.Run("counter/"+name, func(t *testing.T) {
			checkDefinitionCopy(t, get, func(d *pipeline.CounterDef) {
				*d = pipeline.CounterDef{ID: 999, Name: "changed", Size: 1, Unit: p4configv1.CounterSpec_BYTES}
			})
		})
	}
	t.Run("direct counter", func(t *testing.T) {
		checkDefinitionCopy(t, func(p *pipeline.Pipeline) *pipeline.DirectCounterDef {
			d, _ := p.DirectCounter("ingress.direct")
			return d
		}, func(d *pipeline.DirectCounterDef) {
			*d = pipeline.DirectCounterDef{ID: 999, Name: "changed", Unit: p4configv1.CounterSpec_PACKETS, DirectTableID: 999, DirectTableName: "changed"}
		})
	})
	t.Run("meter", func(t *testing.T) {
		checkDefinitionCopy(t, func(p *pipeline.Pipeline) *pipeline.MeterDef { d, _ := p.Meter("ingress.meter"); return d }, func(d *pipeline.MeterDef) {
			*d = pipeline.MeterDef{ID: 999, Name: "changed", Size: 1, Unit: p4configv1.MeterSpec_BYTES}
		})
	})
	t.Run("direct meter", func(t *testing.T) {
		checkDefinitionCopy(t, func(p *pipeline.Pipeline) *pipeline.DirectMeterDef {
			d, _ := p.DirectMeter("ingress.direct_meter")
			return d
		}, func(d *pipeline.DirectMeterDef) {
			*d = pipeline.DirectMeterDef{ID: 999, Name: "changed", Unit: p4configv1.MeterSpec_PACKETS, DirectTableID: 999, DirectTableName: "changed"}
		})
	})
	t.Run("register", func(t *testing.T) {
		checkDefinitionCopy(t, func(p *pipeline.Pipeline) *pipeline.RegisterDef { d, _ := p.Register("ingress.register"); return d }, func(d *pipeline.RegisterDef) {
			*d = pipeline.RegisterDef{ID: 999, Name: "changed", Size: 1}
		})
	})
	for name, get := range map[string]func(*pipeline.Pipeline) *pipeline.DigestDef{
		"name": func(p *pipeline.Pipeline) *pipeline.DigestDef { d, _ := p.Digest("ingress.digest"); return d },
		"ID":   func(p *pipeline.Pipeline) *pipeline.DigestDef { d, _ := p.DigestByID(600); return d },
	} {
		t.Run("digest/"+name, func(t *testing.T) {
			checkDefinitionCopy(t, get, func(d *pipeline.DigestDef) { *d = pipeline.DigestDef{ID: 999, Name: "changed"} })
		})
	}
	for name, get := range map[string]func(*pipeline.Pipeline) *pipeline.ControllerPacketMetadataDef{
		"name": func(p *pipeline.Pipeline) *pipeline.ControllerPacketMetadataDef {
			d, _ := p.PacketMetadata("packet_in")
			return d
		},
		"ID": func(p *pipeline.Pipeline) *pipeline.ControllerPacketMetadataDef {
			d, _ := p.PacketMetadataByID(700)
			return d
		},
	} {
		t.Run("packet metadata/"+name, func(t *testing.T) {
			checkDefinitionCopy(t, get, func(d *pipeline.ControllerPacketMetadataDef) {
				d.ID, d.Name = 999, "changed"
				field, ok := d.Field("ingress_port")
				require.True(t, ok)
				require.Same(t, d.Metadata[0], field)
				byID, ok := d.FieldByID(1)
				require.True(t, ok)
				require.Same(t, field, byID)
				*field = pipeline.PacketMetadataField{ID: 9, Name: "changed", Bitwidth: 1}
				d.Metadata[0] = nil
			})
		})
	}
}

func pipelineRawMessages(p *pipeline.Pipeline) []proto.Message {
	table, _ := p.Table("t_l2")
	action, _ := p.Action("forward")
	counter, _ := p.Counter("pkt_counter")
	directCounter, _ := p.DirectCounter("ingress.direct")
	meter, _ := p.Meter("ingress.meter")
	directMeter, _ := p.DirectMeter("ingress.direct_meter")
	register, _ := p.Register("ingress.register")
	digest, _ := p.Digest("ingress.digest")
	metadata, _ := p.PacketMetadata("packet_in")
	return []proto.Message{table.Raw(), action.Raw(), counter.Raw(), directCounter.Raw(), meter.Raw(), directMeter.Raw(), register.Raw(), digest.Raw(), metadata.Raw()}
}

func TestPipeline_RawCopies(t *testing.T) {
	p, err := pipeline.New(sampleInfo(), []byte{1, 2, 3})
	require.NoError(t, err)
	want := proto.Clone(p.Info())
	for _, raw := range pipelineRawMessages(p) {
		proto.Reset(raw)
	}
	config := p.DeviceConfig()
	config[0] = 99
	require.True(t, proto.Equal(want, p.Info()))
	require.Equal(t, []byte{1, 2, 3}, p.DeviceConfig())
	reference, err := pipeline.New(sampleInfo(), nil)
	require.NoError(t, err)
	for i, raw := range pipelineRawMessages(p) {
		require.True(t, proto.Equal(pipelineRawMessages(reference)[i], raw))
	}
	// Repeated Raw calls from the same definition must also be independent.
	table, _ := p.Table("t_l2")
	proto.Reset(table.Raw())
	require.EqualValues(t, 100, table.Raw().GetPreamble().GetId())
}

func TestPipeline_ConcurrentSnapshots(t *testing.T) {
	info := sampleInfo()
	config := []byte{1, 2, 3}
	want := proto.Clone(info)
	p, err := pipeline.New(info, config)
	require.NoError(t, err)
	start := make(chan struct{})
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := range 100 {
			info.Actions[0].Params[0].Bitwidth = int32(i)
			info.Tables[0].Preamble.Id = uint32(i)
			config[0] = byte(i)
		}
	}()
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 50 {
				if !proto.Equal(want, p.Info()) {
					failures <- fmt.Errorf("P4Info changed during concurrent use")
					return
				}
				table, ok := p.Table("t_l2")
				if !ok || table.MatchFields[0].Bitwidth != 48 {
					failures <- fmt.Errorf("table definition changed during concurrent use")
					return
				}
				table.MatchFields[0].Bitwidth = 1
				action, _ := p.ActionByID(200)
				action.Params[0].Bitwidth = 1
				metadata, _ := p.PacketMetadataByID(700)
				metadata.Metadata[0].Bitwidth = 1
				for _, raw := range pipelineRawMessages(p) {
					proto.Reset(raw)
				}
				proto.Reset(p.Info())
				p.DeviceConfig()[0] = 99
			}
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.True(t, proto.Equal(want, p.Info()))
	require.Equal(t, []byte{1, 2, 3}, p.DeviceConfig())
}
