package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

// UpdateType re-exports p4v1.Update_Type so callers of Write never need to
// import the proto package directly.
type UpdateType = p4v1.Update_Type

// Update constants mirrored from p4v1.
const (
	UpdateInsert UpdateType = p4v1.Update_INSERT
	UpdateModify UpdateType = p4v1.Update_MODIFY
	UpdateDelete UpdateType = p4v1.Update_DELETE
)

// Atomicity re-exports the p4v1 atomicity enum.
type Atomicity = p4v1.WriteRequest_Atomicity

// Atomicity constants.
const (
	AtomicityContinueOnError = p4v1.WriteRequest_CONTINUE_ON_ERROR
	AtomicityRollbackOnError = p4v1.WriteRequest_ROLLBACK_ON_ERROR
	AtomicityDataplaneAtomic = p4v1.WriteRequest_DATAPLANE_ATOMIC
)

// WriteOptions tunes a WriteRequest.
type WriteOptions struct {
	// Atomicity passes through to WriteRequest.atomicity. Zero value means
	// CONTINUE_ON_ERROR (the P4Runtime default).
	Atomicity Atomicity
}

// WriteTableEntry is a convenience wrapper that executes a single
// INSERT / MODIFY / DELETE against a single table entry.
func (c *Client) WriteTableEntry(ctx context.Context, kind UpdateType, entry *p4v1.TableEntry) error {
	return c.Write(ctx, WriteOptions{}, TableEntryUpdate(kind, entry))
}

// TableEntryUpdate builds a p4v1.Update for a table entry.
func TableEntryUpdate(kind UpdateType, entry *p4v1.TableEntry) *p4v1.Update {
	return &p4v1.Update{
		Type: kind,
		Entity: &p4v1.Entity{
			Entity: &p4v1.Entity_TableEntry{TableEntry: entry},
		},
	}
}

// Write issues a P4Runtime WriteRequest containing the supplied updates.
// Returns ErrNotPrimary if the client is not primary at the moment of the
// call. RPC failures return *errs.WriteError, preserving the gRPC status
// and any complete set of per-update results. errors.Is matches known
// sentinels for any failed update.
func (c *Client) Write(ctx context.Context, opts WriteOptions, updates ...*p4v1.Update) error {
	if !c.IsPrimary() {
		return errs.ErrNotPrimary
	}
	if len(updates) == 0 {
		return nil
	}
	req := &p4v1.WriteRequest{
		DeviceId: c.opts.deviceID,
		ElectionId: &p4v1.Uint128{
			High: c.opts.electionID.High,
			Low:  c.opts.electionID.Low,
		},
		Role:      c.opts.role,
		Updates:   updates,
		Atomicity: opts.Atomicity,
	}
	_, err := c.rpc.Write(ctx, req)
	if err != nil {
		return translateWriteError(err, len(updates))
	}
	return nil
}

func translateWriteError(err error, updateCount int) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	return &errs.WriteError{Cause: err, Updates: writeUpdateResults(st, updateCount)}
}

func writeUpdateResults(st *status.Status, updateCount int) []*p4v1.Error {
	// Per-update failures use UNKNOWN with exactly one p4.Error for every
	// update, including successful updates. Never filter details, since
	// doing so would shift their association with request indices.
	details := st.Proto().GetDetails()
	if st.Code() != codes.Unknown || updateCount <= 0 || len(details) != updateCount {
		return nil
	}
	updates := make([]*p4v1.Error, updateCount)
	failed := false
	for i, detail := range details {
		update := new(p4v1.Error)
		if detail == nil || detail.UnmarshalTo(update) != nil ||
			update.GetCanonicalCode() < int32(codes.OK) || update.GetCanonicalCode() > int32(codes.Unauthenticated) {
			return nil
		}
		updates[i] = update
		failed = failed || update.GetCanonicalCode() != int32(codes.OK)
	}
	if !failed {
		return nil
	}
	return updates
}

// ReadTableEntries streams every TableEntry for the given table (or every
// table if tableID is zero). The returned slice is sorted in target-supplied
// order; no further ranking is applied.
func (c *Client) ReadTableEntries(ctx context.Context, tableID uint32) ([]*p4v1.TableEntry, error) {
	return c.readEntries(ctx, &p4v1.Entity{
		Entity: &p4v1.Entity_TableEntry{TableEntry: &p4v1.TableEntry{TableId: tableID}},
	})
}

// Read issues a P4Runtime ReadRequest with the supplied entities and
// collects every streamed response into a single slice of entities.
// It sends the configured role and does not require primary status.
func (c *Client) Read(ctx context.Context, entities ...*p4v1.Entity) ([]*p4v1.Entity, error) {
	if len(entities) == 0 {
		return nil, errors.New("client.Read: no entities supplied")
	}
	req := &p4v1.ReadRequest{
		DeviceId: c.opts.deviceID,
		Role:     c.opts.role,
		Entities: entities,
	}
	stream, err := c.rpc.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Read: %w", err)
	}
	var out []*p4v1.Entity
	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("Read recv: %w", err)
		}
		out = append(out, resp.GetEntities()...)
	}
	return out, nil
}

func (c *Client) readEntries(ctx context.Context, selector *p4v1.Entity) ([]*p4v1.TableEntry, error) {
	ents, err := c.Read(ctx, selector)
	if err != nil {
		return nil, err
	}
	out := make([]*p4v1.TableEntry, 0, len(ents))
	for _, e := range ents {
		if te := e.GetTableEntry(); te != nil {
			out = append(out, te)
		}
	}
	return out, nil
}
