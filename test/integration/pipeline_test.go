//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/codec"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func TestBMv2_PipelineActions(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_MASK_P4INFO"), os.Getenv("P4RT_MASK_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_MASK_P4INFO and P4RT_MASK_DEVICE_CONFIG unset; skipping live pipeline actions")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{Action: client.PipelineVerifyAndSave})
	require.NoError(t, err)
	require.Equal(t, client.PipelineVerifyAndSave, res.Action)
	require.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndSave}, res.Attempted)
	table, ok := p.Table("MyIngress.t_ternary")
	require.True(t, ok)
	require.Len(t, table.MatchFields, 1)
	entry, err := tableentry.NewBuilder(p, "MyIngress.t_ternary").
		Match(table.MatchFields[0].Name, tableentry.Ternary(codec.MustEncodeUint(1, 9), codec.MustEncodeUint(511, 9))).
		Priority(10).
		Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).Build()
	require.NoError(t, err)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
	assertEntry := func(t *testing.T, want bool) {
		t.Helper()
		entries, err := c.ReadTableEntries(ctx, entry.TableId)
		require.NoError(t, err)
		found := false
		for _, candidate := range entries {
			if len(candidate.Match) == 1 && proto.Equal(candidate.Match[0], entry.Match[0]) {
				found = true
				require.True(t, proto.Equal(entry.Action, candidate.Action))
			}
		}
		require.Equal(t, want, found)
	}
	res, err = c.SetPipeline(ctx, nil, client.SetPipelineOptions{Action: client.PipelineCommit})
	require.NoError(t, err)
	require.Equal(t, client.PipelineCommit, res.Action)
	require.Equal(t, []client.SetPipelineAction{client.PipelineCommit}, res.Attempted)
	active, err := c.GetPipeline(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(p.Info(), active.Info()))
	assertEntry(t, true)
	for _, action := range []client.SetPipelineAction{client.PipelineVerify, client.PipelineReconcileAndCommit, client.PipelineVerifyAndCommit} {
		t.Run(action.String(), func(t *testing.T) {
			res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{Action: action, NoFallback: true})
			require.NoError(t, err)
			require.Equal(t, action, res.Action)
			require.Equal(t, []client.SetPipelineAction{action}, res.Attempted)
			assertEntry(t, action != client.PipelineVerifyAndCommit)
		})
		if t.Failed() {
			return
		}
	}
}
