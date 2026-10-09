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
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
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
		{name: "primary rejected by target", inErr: status.Error(codes.PermissionDenied, "Not primary"), wantIs: errs.ErrNotPrimary},
		{name: "pipeline unset", inErr: status.Error(codes.FailedPrecondition, "No forwarding pipeline config set for this device"), wantIs: errs.ErrPipelineNotSet},
		{name: "unsupported feature", inErr: status.Error(codes.Unimplemented, "atomic writes unsupported"), wantIs: errs.ErrTargetUnsupported},
		{name: "role denied", inErr: status.Error(codes.PermissionDenied, "primary role cannot write this table")},
		{name: "unrelated precondition", inErr: status.Error(codes.FailedPrecondition, "primary role has no access to this table")},
		{name: "pipeline mismatch", inErr: status.Error(codes.FailedPrecondition, "pipeline cookie differs")},
		{name: "deadline", inErr: status.Error(codes.DeadlineExceeded, "deadline exceeded")},
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
			require.Error(t, err)
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			} else {
				for _, sentinel := range []error{errs.ErrNotPrimary, errs.ErrPipelineNotSet, errs.ErrEntryExists, errs.ErrEntryNotFound, errs.ErrTargetUnsupported} {
					assert.NotErrorIs(t, err, sentinel)
				}
			}
			assert.Equal(t, status.Code(tc.inErr), status.Code(err))
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.True(t, proto.Equal(status.Convert(tc.inErr).Proto(), st.Proto()))
		})
	}
}

func TestWrite_UpdateResults(t *testing.T) {
	cases := []struct {
		name    string
		results []*p4v1.Error
		wantIs  []error
	}{
		{"duplicate insert", []*p4v1.Error{{CanonicalCode: int32(codes.AlreadyExists), Message: "duplicate"}}, []error{errs.ErrEntryExists}},
		{"missing entry", []*p4v1.Error{{CanonicalCode: int32(codes.NotFound), Message: "missing"}}, []error{errs.ErrEntryNotFound}},
		{"mixed batch", []*p4v1.Error{
			{},
			{CanonicalCode: int32(codes.AlreadyExists), Message: "duplicate", Space: "bmv2-v1model-test", Code: 99},
			{CanonicalCode: int32(codes.NotFound), Message: "missing"},
			{CanonicalCode: int32(codes.Unimplemented), Message: "unsupported"},
			{},
		}, []error{errs.ErrEntryExists, errs.ErrEntryNotFound, errs.ErrTargetUnsupported}},
		{"per-entry permission", []*p4v1.Error{{CanonicalCode: int32(codes.PermissionDenied), Message: "not primary for this role"}}, nil},
		{"per-entry precondition", []*p4v1.Error{{CanonicalCode: int32(codes.FailedPrecondition), Message: "No forwarding pipeline config set"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpcStatus := status.New(codes.Unknown, "Error(s) during Write")
			for _, result := range tc.results {
				var err error
				rpcStatus, err = rpcStatus.WithDetails(result)
				require.NoError(t, err)
			}
			h := testutil.StartServer(t)
			h.Mu.Lock()
			h.OverrideWriteErr = rpcStatus.Err()
			h.Mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h)
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.BecomePrimary(ctx))
			updates := make([]*p4v1.Update, len(tc.results))
			for i := range updates {
				updates[i] = client.TableEntryUpdate(client.UpdateInsert, &p4v1.TableEntry{TableId: uint32(i + 1)})
			}
			err = c.Write(ctx, client.WriteOptions{Atomicity: client.AtomicityRollbackOnError}, updates...)
			require.Error(t, err)
			for _, sentinel := range tc.wantIs {
				assert.ErrorIs(t, err, sentinel)
			}
			assert.NotErrorIs(t, err, errs.ErrNotPrimary)
			assert.NotErrorIs(t, err, errs.ErrPipelineNotSet)
			var writeErr *errs.WriteError
			require.ErrorAs(t, err, &writeErr)
			require.Len(t, writeErr.Updates, len(tc.results))
			for i, want := range tc.results {
				assert.True(t, proto.Equal(want, writeErr.Updates[i]), "result index %d", i)
			}
			assert.True(t, proto.Equal(rpcStatus.Proto(), status.Convert(err).Proto()))
			h.Mu.Lock()
			assert.Equal(t, p4v1.WriteRequest_ROLLBACK_ON_ERROR, h.WriteRequests[0].GetAtomicity())
			assert.Len(t, h.WriteRequests, 1, "Write must not retry a failed batch")
			h.Mu.Unlock()
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
