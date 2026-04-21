// Package pre wraps the P4Runtime Packet Replication Engine (PRE)
// entities: multicast groups and clone sessions.
//
// Multicast groups are referenced from P4 programs through the
// standard_metadata.mcast_grp field (V1Model) or the PSA equivalent. A
// clone session is referenced by clone_session_id (PSA) or
// clone3/clone_preserving_field_list (V1Model).
//
// IDs in this package are target-local: the caller picks them and the
// target accepts any non-zero value, so we do not cross-check against
// the P4Info.
package pre

import (
	"context"
	"errors"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

// Replica is a single replica in a multicast group or clone session.
// Instance distinguishes replicas that share the same egress port.
type Replica struct {
	EgressPort uint32
	Instance   uint32
}

// MulticastGroup describes a PRE multicast group entry.
type MulticastGroup struct {
	ID       uint32
	Replicas []Replica
	Metadata []byte // optional opaque caller metadata (P4Runtime 1.4+)
}

// CloneSession describes a PRE clone session entry. PacketLengthBytes
// truncates every cloned packet to this many bytes; zero disables
// truncation.
type CloneSession struct {
	ID                uint32
	Replicas          []Replica
	ClassOfService    uint32
	PacketLengthBytes int32
}

// Writer is a typed wrapper over Client for PRE operations. It is safe
// for concurrent use because Client is.
type Writer struct {
	c *client.Client
}

// NewWriter constructs a PRE writer bound to c. Returns an error only
// if c is nil.
func NewWriter(c *client.Client) (*Writer, error) {
	if c == nil {
		return nil, errors.New("pre.NewWriter: nil client")
	}
	return &Writer{c: c}, nil
}

// InsertMulticastGroup installs a new multicast group. It is an error
// to reinstall a group with the same ID; use ModifyMulticastGroup for
// updates.
func (w *Writer) InsertMulticastGroup(ctx context.Context, mg MulticastGroup) error {
	return w.writeMulticastGroup(ctx, client.UpdateInsert, mg)
}

// ModifyMulticastGroup replaces the replica set of an existing multicast
// group.
func (w *Writer) ModifyMulticastGroup(ctx context.Context, mg MulticastGroup) error {
	return w.writeMulticastGroup(ctx, client.UpdateModify, mg)
}

// DeleteMulticastGroup removes the multicast group with the given ID.
func (w *Writer) DeleteMulticastGroup(ctx context.Context, id uint32) error {
	if id == 0 {
		return errors.New("pre.DeleteMulticastGroup: id must be non-zero")
	}
	return w.c.Write(ctx, client.WriteOptions{},
		multicastUpdate(client.UpdateDelete, MulticastGroup{ID: id}))
}

// ReadMulticastGroups returns every multicast group installed on the
// target. Pass id=0 to read all groups; pass a specific id to read just
// that one.
func (w *Writer) ReadMulticastGroups(ctx context.Context, id uint32) ([]MulticastGroup, error) {
	selector := &p4v1.Entity{Entity: &p4v1.Entity_PacketReplicationEngineEntry{
		PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
			Type: &p4v1.PacketReplicationEngineEntry_MulticastGroupEntry{
				MulticastGroupEntry: &p4v1.MulticastGroupEntry{MulticastGroupId: id},
			},
		},
	}}
	ents, err := w.c.Read(ctx, selector)
	if err != nil {
		return nil, err
	}
	var out []MulticastGroup
	for _, e := range ents {
		mge := e.GetPacketReplicationEngineEntry().GetMulticastGroupEntry()
		if mge == nil {
			continue
		}
		out = append(out, MulticastGroup{
			ID:       mge.GetMulticastGroupId(),
			Replicas: decodeReplicas(mge.GetReplicas()),
			Metadata: mge.GetMetadata(),
		})
	}
	return out, nil
}

// InsertCloneSession installs a new clone session.
func (w *Writer) InsertCloneSession(ctx context.Context, cs CloneSession) error {
	return w.writeCloneSession(ctx, client.UpdateInsert, cs)
}

// ModifyCloneSession updates an existing clone session.
func (w *Writer) ModifyCloneSession(ctx context.Context, cs CloneSession) error {
	return w.writeCloneSession(ctx, client.UpdateModify, cs)
}

// DeleteCloneSession removes the clone session with the given ID.
func (w *Writer) DeleteCloneSession(ctx context.Context, id uint32) error {
	if id == 0 {
		return errors.New("pre.DeleteCloneSession: id must be non-zero")
	}
	return w.c.Write(ctx, client.WriteOptions{},
		cloneUpdate(client.UpdateDelete, CloneSession{ID: id}))
}

// ReadCloneSessions returns every clone session installed on the target.
// Pass id=0 to read all sessions.
func (w *Writer) ReadCloneSessions(ctx context.Context, id uint32) ([]CloneSession, error) {
	selector := &p4v1.Entity{Entity: &p4v1.Entity_PacketReplicationEngineEntry{
		PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
			Type: &p4v1.PacketReplicationEngineEntry_CloneSessionEntry{
				CloneSessionEntry: &p4v1.CloneSessionEntry{SessionId: id},
			},
		},
	}}
	ents, err := w.c.Read(ctx, selector)
	if err != nil {
		return nil, err
	}
	var out []CloneSession
	for _, e := range ents {
		cse := e.GetPacketReplicationEngineEntry().GetCloneSessionEntry()
		if cse == nil {
			continue
		}
		out = append(out, CloneSession{
			ID:                cse.GetSessionId(),
			Replicas:          decodeReplicas(cse.GetReplicas()),
			ClassOfService:    cse.GetClassOfService(),
			PacketLengthBytes: cse.GetPacketLengthBytes(),
		})
	}
	return out, nil
}

func (w *Writer) writeMulticastGroup(ctx context.Context, kind client.UpdateType, mg MulticastGroup) error {
	if mg.ID == 0 {
		return fmt.Errorf("pre.MulticastGroup: id must be non-zero")
	}
	if err := validateReplicas(mg.Replicas); err != nil {
		return err
	}
	return w.c.Write(ctx, client.WriteOptions{}, multicastUpdate(kind, mg))
}

func (w *Writer) writeCloneSession(ctx context.Context, kind client.UpdateType, cs CloneSession) error {
	if cs.ID == 0 {
		return fmt.Errorf("pre.CloneSession: id must be non-zero")
	}
	if err := validateReplicas(cs.Replicas); err != nil {
		return err
	}
	return w.c.Write(ctx, client.WriteOptions{}, cloneUpdate(kind, cs))
}

func multicastUpdate(kind client.UpdateType, mg MulticastGroup) *p4v1.Update {
	entry := &p4v1.MulticastGroupEntry{
		MulticastGroupId: mg.ID,
		Replicas:         encodeReplicas(mg.Replicas),
	}
	if len(mg.Metadata) > 0 {
		entry.Metadata = append([]byte(nil), mg.Metadata...)
	}
	return &p4v1.Update{
		Type: kind,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_PacketReplicationEngineEntry{
			PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
				Type: &p4v1.PacketReplicationEngineEntry_MulticastGroupEntry{
					MulticastGroupEntry: entry,
				},
			},
		}},
	}
}

func cloneUpdate(kind client.UpdateType, cs CloneSession) *p4v1.Update {
	entry := &p4v1.CloneSessionEntry{
		SessionId:         cs.ID,
		Replicas:          encodeReplicas(cs.Replicas),
		ClassOfService:    cs.ClassOfService,
		PacketLengthBytes: cs.PacketLengthBytes,
	}
	return &p4v1.Update{
		Type: kind,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_PacketReplicationEngineEntry{
			PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
				Type: &p4v1.PacketReplicationEngineEntry_CloneSessionEntry{
					CloneSessionEntry: entry,
				},
			},
		}},
	}
}

func encodeReplicas(rs []Replica) []*p4v1.Replica {
	out := make([]*p4v1.Replica, 0, len(rs))
	for _, r := range rs {
		out = append(out, &p4v1.Replica{
			PortKind: &p4v1.Replica_EgressPort{EgressPort: r.EgressPort},
			Instance: r.Instance,
		})
	}
	return out
}

func decodeReplicas(rs []*p4v1.Replica) []Replica {
	out := make([]Replica, 0, len(rs))
	for _, r := range rs {
		out = append(out, Replica{
			EgressPort: r.GetEgressPort(),
			Instance:   r.GetInstance(),
		})
	}
	return out
}

func validateReplicas(rs []Replica) error {
	if len(rs) == 0 {
		return errors.New("pre: replicas must not be empty")
	}
	for i, r := range rs {
		if r.EgressPort == 0 {
			return fmt.Errorf("pre: replica[%d] egress port must be non-zero", i)
		}
	}
	return nil
}
