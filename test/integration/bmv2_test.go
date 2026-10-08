//go:build integration

package integration

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

// targetAddr is the address of the P4Runtime target under test. It defaults
// to the local BMv2 launched by scripts/run-bmv2.sh and can be overridden
// with P4RT_TARGET.
func targetAddr() string {
	if a := os.Getenv("P4RT_TARGET"); a != "" {
		return a
	}
	return "127.0.0.1:9559"
}

// p4infoPath points at the committed fixture that matches the bundled L2
// program.
func p4infoPath() string {
	if p := os.Getenv("P4RT_P4INFO"); p != "" {
		return p
	}
	return "../../examples/testdata/l2.p4info.txt"
}

// deviceConfigPath is optional — set P4RT_DEVICE_CONFIG to a locally built
// bmv2.json. When unset the test only exercises VERIFY_AND_COMMIT and is
// skipped automatically if the target rejects an empty config.
func deviceConfigPath() string {
	return os.Getenv("P4RT_DEVICE_CONFIG")
}

func TestBMv2_ConnectAndSetPipeline(t *testing.T) {
	infoBytes, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	var configBytes []byte
	if cfg := deviceConfigPath(); cfg != "" {
		configBytes, err = os.ReadFile(cfg)
		require.NoError(t, err)
	} else {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live pipeline push")
	}
	p, err := pipeline.LoadText(infoBytes, configBytes)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(),
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
	)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)

	entry, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	require.NoError(t, err)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))

	entries, err := c.ReadTableEntries(ctx, entry.GetTableId())
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	t.Run("duplicate insert", func(t *testing.T) {
		err := c.WriteTableEntry(ctx, client.UpdateInsert, entry)
		require.ErrorIs(t, err, errs.ErrEntryExists)
		require.Equal(t, codes.Unknown, status.Code(err))
		var writeErr *errs.WriteError
		require.ErrorAs(t, err, &writeErr)
		require.Len(t, writeErr.Updates, 1)
		require.EqualValues(t, codes.AlreadyExists, writeErr.Updates[0].GetCanonicalCode())
	})

	missing, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:66"))).
		Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).Build()
	require.NoError(t, err)
	t.Run("missing entry", func(t *testing.T) {
		for _, kind := range []client.UpdateType{client.UpdateModify, client.UpdateDelete} {
			err := c.WriteTableEntry(ctx, kind, missing)
			require.ErrorIs(t, err, errs.ErrEntryNotFound)
			require.Equal(t, codes.Unknown, status.Code(err))
			var writeErr *errs.WriteError
			require.ErrorAs(t, err, &writeErr)
			require.Len(t, writeErr.Updates, 1)
			require.EqualValues(t, codes.NotFound, writeErr.Updates[0].GetCanonicalCode())
		}
	})

	t.Run("partially successful batch", func(t *testing.T) {
		fresh, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
			Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:77"))).
			Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(2, 9))).Build()
		require.NoError(t, err)
		err = c.Write(ctx, client.WriteOptions{},
			client.TableEntryUpdate(client.UpdateInsert, fresh),
			client.TableEntryUpdate(client.UpdateInsert, entry),
			client.TableEntryUpdate(client.UpdateDelete, missing),
		)
		require.ErrorIs(t, err, errs.ErrEntryExists)
		require.ErrorIs(t, err, errs.ErrEntryNotFound)
		require.Equal(t, codes.Unknown, status.Code(err))
		var writeErr *errs.WriteError
		require.ErrorAs(t, err, &writeErr)
		require.Len(t, writeErr.Updates, 3)
		for i, code := range []codes.Code{codes.OK, codes.AlreadyExists, codes.NotFound} {
			require.EqualValues(t, code, writeErr.Updates[i].GetCanonicalCode(), "update %d", i)
		}
		entries, err := c.ReadTableEntries(ctx, fresh.TableId)
		require.NoError(t, err)
		var stored *p4v1.TableEntry
		for _, candidate := range entries {
			if len(candidate.Match) == 1 && proto.Equal(candidate.Match[0], fresh.Match[0]) {
				stored = candidate
			}
		}
		require.NotNil(t, stored, "successful update was not stored")
		require.True(t, proto.Equal(fresh.Action, stored.Action))
		key := &p4v1.TableEntry{TableId: fresh.TableId, Match: fresh.Match}
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
	})

	t.Run("zero match and action parameter", func(t *testing.T) {
		zero, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
			Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:00:00:00:00:00"))).
			Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(0, 9))).
			Build()
		require.NoError(t, err)
		require.Equal(t, []byte{0x00}, zero.Match[0].GetExact().Value)
		require.Equal(t, []byte{0x00}, zero.GetAction().GetAction().Params[0].Value)
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, zero))

		entries, err := c.ReadTableEntries(ctx, zero.GetTableId())
		require.NoError(t, err)
		var stored *p4v1.TableEntry
		for _, candidate := range entries {
			if len(candidate.Match) == 1 && proto.Equal(candidate.Match[0], zero.Match[0]) {
				stored = candidate
				break
			}
		}
		require.NotNil(t, stored, "zero-valued entry was not returned by the target")
		require.Len(t, stored.GetAction().GetAction().Params, 1)
		require.Equal(t, zero.GetAction().GetAction().ActionId, stored.GetAction().GetAction().ActionId)
		require.Equal(t, zero.GetAction().GetAction().Params[0].ParamId, stored.GetAction().GetAction().Params[0].ParamId)
		require.Equal(t, []byte{0x00}, stored.GetAction().GetAction().Params[0].Value)

		key := &p4v1.TableEntry{TableId: zero.TableId, Match: zero.Match}
		require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
		entries, err = c.ReadTableEntries(ctx, zero.GetTableId())
		require.NoError(t, err)
		for _, candidate := range entries {
			if len(candidate.Match) == 1 {
				require.False(t, proto.Equal(candidate.Match[0], zero.Match[0]), "zero-valued entry remains after deletion")
			}
		}
	})

	require.NoError(t, c.Close())
	require.Equal(t, client.StateDisconnected, c.State())
	require.False(t, c.IsPrimary())
	require.ErrorIs(t, c.WriteTableEntry(ctx, client.UpdateModify, entry), errs.ErrNotPrimary)
	require.ErrorIs(t, c.BecomePrimary(ctx), errs.ErrStreamClosed)
}

func TestBMv2_ConcurrentPrimaryWaiters(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live arbitration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(id uint64) *client.Client {
		c, err := client.Dial(ctx, targetAddr(),
			client.WithDeviceID(1),
			client.WithElectionID(client.ElectionID{Low: id}),
			client.WithInsecure(),
		)
		require.NoError(t, err)
		return c
	}
	lower := dial(10)
	defer lower.Close()
	require.True(t, lower.IsPrimary())
	higher := dial(20)
	defer higher.Close()
	require.True(t, higher.IsPrimary())
	require.Eventually(t, func() bool {
		return lower.State() == client.StateBackup
	}, time.Second, 10*time.Millisecond)

	waitCtx, stopWait := context.WithTimeout(ctx, 100*time.Millisecond)
	require.ErrorIs(t, lower.BecomePrimary(waitCtx), context.DeadlineExceeded)
	stopWait()

	var primaryEvents atomic.Int32
	backupSeen := make(chan struct{})
	go func() {
		seenBackup := false
		for ev := range lower.Events() {
			if ev.State == client.StatePrimary {
				primaryEvents.Add(1)
			}
			if ev.State == client.StateBackup && !seenBackup {
				seenBackup = true
				close(backupSeen)
			}
		}
	}()
	select {
	case <-backupSeen:
	case <-ctx.Done():
		t.Fatal("event consumer did not observe backup state")
	}
	before := primaryEvents.Load()
	require.Greater(t, before, int32(0))
	const count = 16
	ready := make(chan struct{}, count)
	results := make(chan error, count)
	for range count {
		go func() {
			ready <- struct{}{}
			results <- lower.BecomePrimary(ctx)
		}()
	}
	for range count {
		<-ready
	}
	require.NoError(t, higher.Close())
	for range count {
		require.NoError(t, <-results)
	}
	require.True(t, lower.IsPrimary())
	require.Eventually(t, func() bool {
		return primaryEvents.Load() > before
	}, time.Second, 10*time.Millisecond)
}
