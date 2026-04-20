package client_test

import (
	"context"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
)

func TestWrite_HappyPath(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	err = c.WriteTableEntry(ctx, client.UpdateInsert, &p4v1.TableEntry{TableId: 1})
	require.NoError(t, err)

	h.Mu.Lock()
	require.Len(t, h.WriteRequests, 1)
	rec := h.WriteRequests[0]
	h.Mu.Unlock()
	require.Len(t, rec.Updates, 1)
	assert.Equal(t, p4v1.Update_INSERT, rec.Updates[0].Type)
	assert.EqualValues(t, 1, rec.Updates[0].GetEntity().GetTableEntry().TableId)
}

func TestWrite_NotPrimary(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.PrimaryElectionHigh = 0
	h.PrimaryElectionLow = 99
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	require.Eventually(t, func() bool { return c.State() == client.StateBackup },
		time.Second, 10*time.Millisecond)

	err = c.WriteTableEntry(ctx, client.UpdateInsert, &p4v1.TableEntry{TableId: 1})
	assert.ErrorIs(t, err, errs.ErrNotPrimary)
}

func TestWrite_NoUpdatesIsNoOp(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	require.NoError(t, c.Write(ctx, client.WriteOptions{}))
	h.Mu.Lock()
	assert.Empty(t, h.WriteRequests)
	h.Mu.Unlock()
}

func TestWrite_ErrorTranslation(t *testing.T) {
	cases := []struct {
		name   string
		inErr  error
		wantIs error
	}{
		{name: "already exists", inErr: status.Error(codes.AlreadyExists, "dup"), wantIs: errs.ErrEntryExists},
		{name: "not found", inErr: status.Error(codes.NotFound, "nope"), wantIs: errs.ErrEntryNotFound},
		{name: "not primary", inErr: status.Error(codes.FailedPrecondition, "not primary controller"), wantIs: errs.ErrNotPrimary},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testutil.StartServer(t)
			h.Mu.Lock()
			h.OverrideWriteErr = tc.inErr
			h.Mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h)
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.BecomePrimary(ctx))

			err = c.WriteTableEntry(ctx, client.UpdateInsert, &p4v1.TableEntry{TableId: 1})
			assert.ErrorIs(t, err, tc.wantIs)
		})
	}
}

func TestReadTableEntries(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_TableEntry{TableEntry: &p4v1.TableEntry{TableId: 7}},
		}, {
			Entity: &p4v1.Entity_TableEntry{TableEntry: &p4v1.TableEntry{TableId: 7, Priority: 5}},
		}},
	}}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	entries, err := c.ReadTableEntries(ctx, 7)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.EqualValues(t, 7, entries[0].TableId)
	assert.EqualValues(t, 5, entries[1].Priority)
}

func TestRead_EmptyEntitiesErrors(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	_, err = c.Read(ctx)
	assert.Error(t, err)
}

func TestTableEntryUpdateHelper(t *testing.T) {
	u := client.TableEntryUpdate(client.UpdateDelete, &p4v1.TableEntry{TableId: 3})
	assert.Equal(t, p4v1.Update_DELETE, u.Type)
	assert.EqualValues(t, 3, u.GetEntity().GetTableEntry().TableId)
}
