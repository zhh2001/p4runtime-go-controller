//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func TestBMv2_TableValidation(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_KEY_P4INFO"), os.Getenv("P4RT_KEY_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_KEY_P4INFO and P4RT_KEY_DEVICE_CONFIG unset; skipping live table validation")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)

	readDefault := func(t *testing.T, table string) *p4v1.TableEntry {
		t.Helper()
		key, err := tableentry.NewBuilder(p, table).AsDefault().BuildKey()
		require.NoError(t, err)
		entities, err := c.Read(ctx, &p4v1.Entity{Entity: &p4v1.Entity_TableEntry{TableEntry: key}})
		require.NoError(t, err)
		require.Len(t, entities, 1)
		entry := entities[0].GetTableEntry()
		require.NotNil(t, entry)
		require.True(t, entry.IsDefaultAction)
		return entry
	}
	build := func(table, action string, def bool, timeout int64) (*p4v1.TableEntry, error) {
		b := tableentry.NewBuilder(p, table).Match("s.ingress_port", tableentry.Exact([]byte{1})).
			Action(action, tableentry.Param("port", []byte{2})).IdleTimeout(timeout)
		if def {
			b.AsDefault()
		}
		return b.Build()
	}

	t.Run("action scope", func(t *testing.T) {
		td, ok := p.Table("MyIngress.t_scoped")
		require.True(t, ok)
		require.Equal(t, p4configv1.Table_NOTIFY_CONTROL, td.Raw().GetIdleTimeoutBehavior())
		for _, tc := range []struct {
			action string
			scope  p4configv1.ActionRef_Scope
		}{
			{"MyIngress.entry_only", p4configv1.ActionRef_TABLE_ONLY},
			{"MyIngress.default_only", p4configv1.ActionRef_DEFAULT_ONLY},
		} {
			action, ok := p.Action(tc.action)
			require.True(t, ok)
			found := false
			for _, ref := range td.ActionRefs {
				if ref.ID == action.ID {
					require.Equal(t, tc.scope, ref.Scope)
					found = true
				}
			}
			require.True(t, found, "compiler must emit the action scope")
		}
		for _, action := range []string{"MyIngress.forward", "MyIngress.entry_only"} {
			entry, err := build(td.Name, action, false, 0)
			require.NoError(t, err)
			require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
			stored, err := c.ReadTableEntries(ctx, td.ID)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			require.True(t, proto.Equal(entry.Action, stored[0].Action))
			key, err := tableentry.NewBuilder(p, td.Name).Match("s.ingress_port", tableentry.Exact([]byte{1})).BuildKey()
			require.NoError(t, err)
			require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
		}
		for _, action := range []string{"MyIngress.forward", "MyIngress.default_only"} {
			entry, err := build(td.Name, action, true, 0)
			require.NoError(t, err)
			require.NoError(t, c.WriteTableEntry(ctx, client.UpdateModify, entry))
			require.True(t, proto.Equal(entry.Action, readDefault(t, td.Name).Action))
		}
		for _, tc := range []struct {
			table, action, want string
			def                 bool
		}{
			{td.Name, "MyIngress.entry_only", "TABLE_ONLY", true},
			{td.Name, "MyIngress.default_only", "DEFAULT_ONLY", false},
			{"MyIngress.t_exact", "MyIngress.entry_only", "not on table", false},
		} {
			entry, err := build(tc.table, tc.action, tc.def, 0)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, entry)
		}
	})

	t.Run("constant and indirect tables", func(t *testing.T) {
		before := readDefault(t, "MyIngress.t_const_default")
		entry, err := build("MyIngress.t_const_default", "MyIngress.forward", true, 0)
		require.ErrorContains(t, err, "constant default")
		require.Nil(t, entry)
		require.True(t, proto.Equal(before.Action, readDefault(t, "MyIngress.t_const_default").Action))
		entry, err = build("MyIngress.t_constant", "MyIngress.forward", false, 0)
		require.ErrorContains(t, err, "constant table")
		require.Nil(t, entry)
		entry, err = build("MyIngress.t_constant", "MyIngress.forward", true, 0)
		require.NoError(t, err)
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateModify, entry))
		require.True(t, proto.Equal(entry.Action, readDefault(t, "MyIngress.t_constant").Action))
		for _, def := range []bool{false, true} {
			entry, err = build("MyIngress.t_indirect", "MyIngress.forward", def, 0)
			require.ErrorContains(t, err, "indirect table")
			require.Nil(t, entry)
		}
	})

	t.Run("idle notification", func(t *testing.T) {
		for _, tc := range []struct {
			table   string
			def     bool
			timeout int64
			want    string
		}{
			{"MyIngress.t_scoped", false, -1, "negative"},
			{"MyIngress.t_exact", false, 1, "idle timeout"},
			{"MyIngress.t_scoped", true, 1, "default"},
		} {
			entry, err := build(tc.table, "MyIngress.forward", tc.def, tc.timeout)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, entry)
		}
		entry, err := tableentry.NewBuilder(p, "MyIngress.t_scoped").Match("s.ingress_port", tableentry.Exact([]byte{3})).
			Action("MyIngress.entry_only", tableentry.Param("port", []byte{1})).
			IdleTimeout(100_000_000).Metadata([]byte("idle entry")).Build()
		require.NoError(t, err)
		notifications := make(chan *p4v1.TableEntry, 1)
		stop := c.OnIdleTimeout(func(_ context.Context, msg *p4v1.IdleTimeoutNotification) {
			for _, candidate := range msg.TableEntry {
				if candidate.TableId == entry.TableId {
					select {
					case notifications <- candidate:
					default:
					}
				}
			}
		})
		defer stop()
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
		stored, err := c.ReadTableEntries(ctx, entry.TableId)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.Equal(t, entry.IdleTimeoutNs, stored[0].IdleTimeoutNs)
		require.Equal(t, entry.Metadata, stored[0].Metadata)
		select {
		case expired := <-notifications:
			require.Equal(t, entry.IdleTimeoutNs, expired.IdleTimeoutNs)
			if len(expired.Metadata) == 0 {
				t.Log("PI omitted metadata from the idle notification; Read returned it correctly")
			} else {
				require.Equal(t, entry.Metadata, expired.Metadata)
			}
			require.Len(t, expired.Match, 1)
			require.True(t, proto.Equal(entry.Match[0], expired.Match[0]))
		case <-ctx.Done():
			t.Fatal("idle timeout notification did not arrive")
		}
		stored, err = c.ReadTableEntries(ctx, entry.TableId)
		require.NoError(t, err)
		require.Len(t, stored, 1, "NOTIFY_CONTROL must leave removal to the controller")
		key, err := tableentry.NewBuilder(p, "MyIngress.t_scoped").Match("s.ingress_port", tableentry.Exact([]byte{3})).BuildKey()
		require.NoError(t, err)
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
	})
}
