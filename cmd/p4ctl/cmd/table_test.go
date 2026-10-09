package cmd

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

func tableFixture(t *testing.T) string {
	t.Helper()
	info := &p4configv1.P4Info{
		Actions: []*p4configv1.Action{{
			Preamble: &p4configv1.Preamble{Id: 0x01000001, Name: "ingress.forward"},
			Params:   []*p4configv1.Action_Param{{Id: 1, Name: "port", Bitwidth: 9}},
		}},
	}
	for i, kind := range []p4configv1.MatchField_MatchType{
		p4configv1.MatchField_EXACT, p4configv1.MatchField_LPM,
		p4configv1.MatchField_TERNARY, p4configv1.MatchField_RANGE, p4configv1.MatchField_OPTIONAL,
	} {
		width := int32(9)
		if kind == p4configv1.MatchField_LPM {
			width = 32
		}
		info.Tables = append(info.Tables, &p4configv1.Table{
			Preamble: &p4configv1.Preamble{Id: 0x02000001 + uint32(i), Name: "ingress.t_" + kind.String()},
			MatchFields: []*p4configv1.MatchField{{
				Id: 1, Name: "key", Bitwidth: width,
				Match: &p4configv1.MatchField_MatchType_{MatchType: kind},
			}},
			ActionRefs: []*p4configv1.ActionRef{{Id: 0x01000001}},
		})
	}
	path := filepath.Join(t.TempDir(), "table.p4info.txtpb")
	require.NoError(t, os.WriteFile(path, []byte(prototext.Format(info)), 0o600))
	return path
}

func setTableFlags(t *testing.T, info, table string, matches []string, priority int32) {
	t.Helper()
	name, oldMatches, action, params, p4info, oldPriority := tableName, tableMatches, tableAction, tableParams, tableP4Info, tablePriority
	t.Cleanup(func() {
		tableName, tableMatches, tableAction, tableParams, tableP4Info, tablePriority = name, oldMatches, action, params, p4info, oldPriority
	})
	tableName, tableMatches, tableP4Info, tablePriority = table, matches, info, priority
	tableAction, tableParams = "", nil
}

func TestTableDelete_Request(t *testing.T) {
	info := tableFixture(t)
	for _, tc := range []struct {
		name     string
		table    string
		matches  []string
		priority int32
		want     *p4v1.TableEntry
	}{
		{"exact zero", "ingress.t_EXACT", []string{"key=0"}, 0, &p4v1.TableEntry{TableId: 0x02000001, Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Exact_{Exact: &p4v1.FieldMatch_Exact{Value: []byte{0}}}}}}},
		{"LPM masked suffix", "ingress.t_LPM", []string{"key=10.9.8.7/8"}, 0, &p4v1.TableEntry{TableId: 0x02000002, Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Lpm{Lpm: &p4v1.FieldMatch_LPM{Value: []byte{10, 0, 0, 0}, PrefixLen: 8}}}}}},
		{"LPM wildcard", "ingress.t_LPM", []string{"key=0/0"}, 0, &p4v1.TableEntry{TableId: 0x02000002}},
		{"ternary canonical mask", "ingress.t_TERNARY", []string{"key=0x01ff&0x00ff"}, 10, &p4v1.TableEntry{TableId: 0x02000003, Priority: 10, Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Ternary_{Ternary: &p4v1.FieldMatch_Ternary{Value: []byte{255}, Mask: []byte{255}}}}}}},
		{"ternary wildcard", "ingress.t_TERNARY", []string{"key=0&0"}, 20, &p4v1.TableEntry{TableId: 0x02000003, Priority: 20}},
		{"range", "ingress.t_RANGE", []string{"key=1..3"}, 30, &p4v1.TableEntry{TableId: 0x02000004, Priority: 30, Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Range_{Range: &p4v1.FieldMatch_Range{Low: []byte{1}, High: []byte{3}}}}}}},
		{"optional zero", "ingress.t_OPTIONAL", []string{"key=?0"}, 40, &p4v1.TableEntry{TableId: 0x02000005, Priority: 40, Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Optional_{Optional: &p4v1.FieldMatch_Optional{Value: []byte{0}}}}}}},
		{"optional wildcard", "ingress.t_OPTIONAL", nil, math.MaxInt32, &p4v1.TableEntry{TableId: 0x02000005, Priority: math.MaxInt32}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, addr := startDialServer(t)
			setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 7, Role: "test-role", Insecure: true})
			setTableFlags(t, info, tc.table, tc.matches, tc.priority)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			require.NoError(t, tableWrite(cmd, client.UpdateDelete))
			mock.Mu.Lock()
			defer mock.Mu.Unlock()
			require.Len(t, mock.WriteRequests, 1)
			req := mock.WriteRequests[0]
			assert.EqualValues(t, 1, req.DeviceId)
			assert.EqualValues(t, 7, req.GetElectionId().GetLow())
			assert.Equal(t, "test-role", req.Role)
			require.Len(t, req.Updates, 1)
			assert.Equal(t, client.UpdateDelete, req.Updates[0].Type)
			assert.True(t, proto.Equal(tc.want, req.Updates[0].GetEntity().GetTableEntry()), "unexpected deletion key: %v", req.Updates[0])
		})
	}
}

func TestTableDelete_PriorityFlag(t *testing.T) {
	flag := tableDeleteCmd.Flags().Lookup("priority")
	require.NotNil(t, flag)
	oldPriority, changed := tablePriority, flag.Changed
	t.Cleanup(func() { tablePriority, flag.Changed = oldPriority, changed })
	require.NoError(t, tableDeleteCmd.ParseFlags([]string{"--priority", "123"}))
	assert.EqualValues(t, 123, tablePriority)
}

func TestTableWrite_InsertAndModify(t *testing.T) {
	info := tableFixture(t)
	for _, kind := range []client.UpdateType{client.UpdateInsert, client.UpdateModify} {
		t.Run(kind.String(), func(t *testing.T) {
			mock, addr := startDialServer(t)
			setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
			setTableFlags(t, info, "ingress.t_TERNARY", []string{"key=1&255"}, 10)
			tableAction, tableParams = "ingress.forward", []string{"port=2"}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			require.NoError(t, tableWrite(cmd, kind))
			mock.Mu.Lock()
			defer mock.Mu.Unlock()
			require.Len(t, mock.WriteRequests, 1)
			update := mock.WriteRequests[0].Updates[0]
			assert.Equal(t, kind, update.Type)
			entry := update.GetEntity().GetTableEntry()
			assert.EqualValues(t, 10, entry.Priority)
			action := entry.GetAction().GetAction()
			require.NotNil(t, action)
			assert.EqualValues(t, 0x01000001, action.ActionId)
			require.Len(t, action.Params, 1)
			assert.Equal(t, []byte{2}, action.Params[0].Value)
		})
	}
}

func TestTableDelete_WriteError(t *testing.T) {
	mock, addr := startDialServer(t)
	mock.Mu.Lock()
	mock.OverrideWriteErr = status.Error(codes.NotFound, "entry absent")
	mock.Mu.Unlock()
	setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
	setTableFlags(t, tableFixture(t), "ingress.t_EXACT", []string{"key=1"}, 0)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := tableWrite(cmd, client.UpdateDelete)
	require.ErrorIs(t, err, errs.ErrEntryNotFound)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestTableWrite_InvalidKey(t *testing.T) {
	info := tableFixture(t)
	for _, kind := range []client.UpdateType{client.UpdateInsert, client.UpdateModify, client.UpdateDelete} {
		t.Run(kind.String(), func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				table    string
				matches  []string
				priority int32
				want     string
			}{
				{"missing exact", "ingress.t_EXACT", nil, 0, "exact field"},
				{"missing priority", "ingress.t_TERNARY", nil, 0, "positive priority"},
				{"negative priority", "ingress.t_RANGE", []string{"key=1..3"}, -1, "positive priority"},
				{"priority on exact", "ingress.t_EXACT", []string{"key=1"}, 1, "zero priority"},
				{"priority on LPM", "ingress.t_LPM", []string{"key=0/0"}, 1, "zero priority"},
				{"unknown table", "unknown", nil, 0, "not in pipeline"},
				{"unknown field", "ingress.t_EXACT", []string{"unknown=1"}, 0, "not on table"},
				{"wrong kind", "ingress.t_EXACT", []string{"key=1&1"}, 0, "expects EXACT"},
				{"wide value", "ingress.t_EXACT", []string{"key=512"}, 0, "exceeds 9-bit"},
				{"malformed match", "ingress.t_EXACT", []string{"key"}, 0, "expected field=value"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					mock, addr := startDialServer(t)
					setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
					setTableFlags(t, info, tc.table, tc.matches, tc.priority)
					if kind != client.UpdateDelete {
						tableAction, tableParams = "ingress.forward", []string{"port=1"}
					}
					cmd := &cobra.Command{}
					cmd.SetContext(context.Background())
					require.ErrorContains(t, tableWrite(cmd, kind), tc.want)
					mock.Mu.Lock()
					defer mock.Mu.Unlock()
					assert.Empty(t, mock.WriteRequests)
					assert.Zero(t, mock.ArbitrationEchoed, "invalid keys should be rejected before connecting")
				})
			}
		})
	}
}
