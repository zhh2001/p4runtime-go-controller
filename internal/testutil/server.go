// Package testutil provides an in-process mock P4Runtime server backed by
// google.golang.org/grpc/test/bufconn. It is the default test harness for
// the p4runtime-go-controller SDK — no network sockets are created.
package testutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// MockServer is an in-process P4Runtime server. It implements only the subset
// of methods needed by the SDK unit tests. Behavior is configurable through
// public fields; hold MockServer.Mu while mutating them.
type MockServer struct {
	p4v1.UnimplementedP4RuntimeServer

	Mu                    sync.Mutex
	DeviceID              uint64
	PrimaryElectionHigh   uint64
	PrimaryElectionLow    uint64
	ArbitrationEchoed     int
	WriteRequests         []*p4v1.WriteRequest
	LastCapabilitiesReq   *p4v1.CapabilitiesRequest
	SetPipelineReq        *p4v1.SetForwardingPipelineConfigRequest
	GetPipelineResp       *p4v1.GetForwardingPipelineConfigResponse
	OverrideWriteErr      error
	OverrideReadResp      []*p4v1.ReadResponse
	ArbitrationRejection  *rpcstatus.Status // optional: forces a non-primary response for the first arb
	SetPipelineErrByAction map[p4v1.SetForwardingPipelineConfigRequest_Action]error
	SetPipelineAttempts   []p4v1.SetForwardingPipelineConfigRequest_Action
	PacketsToSend         []*p4v1.StreamMessageResponse
	RecvMu                sync.Mutex
	LastArbitrationUpdate *p4v1.MasterArbitrationUpdate
	ReceivedPacketOuts    []*p4v1.PacketOut
	ReceivedDigestAcks    []*p4v1.DigestListAck
	liveStream            p4v1.P4Runtime_StreamChannelServer
	liveMu                sync.Mutex
}

// NewMockServer returns a MockServer with default (permissive) behavior.
func NewMockServer() *MockServer {
	return &MockServer{DeviceID: 1}
}

// StreamChannel implements the bidirectional P4Runtime StreamChannel. On the
// first request that carries a MasterArbitrationUpdate the mock replies with
// a primary/backup decision, then echoes any PacketsToSend slot the caller
// queued and drops any other incoming messages to make room for the next
// request.
func (m *MockServer) StreamChannel(stream p4v1.P4Runtime_StreamChannelServer) error {
	// Receive the arbitration message first.
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	arb := req.GetArbitration()
	if arb == nil {
		return errors.New("first stream message must be arbitration")
	}
	m.Mu.Lock()
	m.ArbitrationEchoed++
	m.LastArbitrationUpdate = arb
	primary := m.PrimaryElectionHigh == arb.GetElectionId().GetHigh() &&
		m.PrimaryElectionLow == arb.GetElectionId().GetLow()
	if m.PrimaryElectionHigh == 0 && m.PrimaryElectionLow == 0 {
		// Anybody gets primary if not pinned.
		m.PrimaryElectionHigh = arb.GetElectionId().GetHigh()
		m.PrimaryElectionLow = arb.GetElectionId().GetLow()
		primary = true
	}
	rejection := m.ArbitrationRejection
	pktQueue := append([]*p4v1.StreamMessageResponse(nil), m.PacketsToSend...)
	m.PacketsToSend = nil
	m.Mu.Unlock()

	respArb := &p4v1.MasterArbitrationUpdate{
		DeviceId:   arb.GetDeviceId(),
		ElectionId: arb.GetElectionId(),
		Status:     &rpcstatus.Status{Code: int32(codes.OK)},
	}
	switch {
	case rejection != nil:
		respArb.Status = rejection
	case !primary:
		respArb.Status = &rpcstatus.Status{Code: int32(codes.AlreadyExists), Message: "backup"}
	}
	if err := stream.Send(&p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Arbitration{Arbitration: respArb},
	}); err != nil {
		return err
	}

	// Flush queued packets.
	for _, msg := range pktQueue {
		if err := stream.Send(msg); err != nil {
			return err
		}
	}

	m.liveMu.Lock()
	m.liveStream = stream
	m.liveMu.Unlock()
	defer func() {
		m.liveMu.Lock()
		m.liveStream = nil
		m.liveMu.Unlock()
	}()

	// Drain further client messages until the stream closes, recording
	// packet-outs and digest acks for tests to inspect.
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		switch u := msg.GetUpdate().(type) {
		case *p4v1.StreamMessageRequest_Packet:
			m.Mu.Lock()
			m.ReceivedPacketOuts = append(m.ReceivedPacketOuts, u.Packet)
			m.Mu.Unlock()
		case *p4v1.StreamMessageRequest_DigestAck:
			m.Mu.Lock()
			m.ReceivedDigestAcks = append(m.ReceivedDigestAcks, u.DigestAck)
			m.Mu.Unlock()
		}
	}
}

// PushStreamMessage sends a StreamMessageResponse on the currently live
// stream. Tests call this after the client has registered its handlers so
// the in-flight dispatcher can observe the event.
func (m *MockServer) PushStreamMessage(msg *p4v1.StreamMessageResponse) error {
	m.liveMu.Lock()
	s := m.liveStream
	m.liveMu.Unlock()
	if s == nil {
		return fmt.Errorf("testutil: no live stream")
	}
	return s.Send(msg)
}

// Write accepts a WriteRequest and records it. Override the behavior via
// MockServer.OverrideWriteErr.
func (m *MockServer) Write(_ context.Context, req *p4v1.WriteRequest) (*p4v1.WriteResponse, error) {
	m.Mu.Lock()
	m.WriteRequests = append(m.WriteRequests, req)
	err := m.OverrideWriteErr
	m.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &p4v1.WriteResponse{}, nil
}

// Capabilities returns a fixed P4Runtime version string. Callers can inspect
// MockServer.LastCapabilitiesReq after the call.
func (m *MockServer) Capabilities(_ context.Context, req *p4v1.CapabilitiesRequest) (*p4v1.CapabilitiesResponse, error) {
	m.Mu.Lock()
	m.LastCapabilitiesReq = req
	m.Mu.Unlock()
	return &p4v1.CapabilitiesResponse{P4RuntimeApiVersion: "1.3.0"}, nil
}

// SetForwardingPipelineConfig records the request and returns an empty
// response unless SetPipelineErrByAction is configured for the requested
// action, in which case the configured error is returned instead. Every
// attempt is appended to SetPipelineAttempts.
func (m *MockServer) SetForwardingPipelineConfig(_ context.Context, req *p4v1.SetForwardingPipelineConfigRequest) (*p4v1.SetForwardingPipelineConfigResponse, error) {
	m.Mu.Lock()
	m.SetPipelineReq = req
	m.SetPipelineAttempts = append(m.SetPipelineAttempts, req.GetAction())
	err := m.SetPipelineErrByAction[req.GetAction()]
	m.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &p4v1.SetForwardingPipelineConfigResponse{}, nil
}

// GetForwardingPipelineConfig replays GetPipelineResp if configured, or
// returns an empty response otherwise.
func (m *MockServer) GetForwardingPipelineConfig(_ context.Context, _ *p4v1.GetForwardingPipelineConfigRequest) (*p4v1.GetForwardingPipelineConfigResponse, error) {
	m.Mu.Lock()
	resp := m.GetPipelineResp
	m.Mu.Unlock()
	if resp != nil {
		return resp, nil
	}
	return &p4v1.GetForwardingPipelineConfigResponse{}, nil
}

// Read streams back the configured responses (from OverrideReadResp).
func (m *MockServer) Read(_ *p4v1.ReadRequest, stream p4v1.P4Runtime_ReadServer) error {
	m.Mu.Lock()
	resp := append([]*p4v1.ReadResponse(nil), m.OverrideReadResp...)
	m.Mu.Unlock()
	for _, r := range resp {
		if err := stream.Send(r); err != nil {
			return err
		}
	}
	return nil
}

// ServerHarness wraps a MockServer with a bufconn listener and an in-process
// gRPC server. It is goroutine-safe and self-cleaning; callers register
// MockServer overrides via the embedded *MockServer pointer.
type ServerHarness struct {
	*MockServer
	gRPC     *grpc.Server
	listener *bufconn.Listener
	done     chan struct{}
}

// StartServer boots a fresh MockServer over bufconn and registers a
// cleanup handler on the test. The returned harness exposes the listener via
// Dial helpers.
func StartServer(t *testing.T) *ServerHarness {
	t.Helper()
	ms := NewMockServer()
	lis := bufconn.Listen(bufSize)
	gsrv := grpc.NewServer()
	p4v1.RegisterP4RuntimeServer(gsrv, ms)
	h := &ServerHarness{MockServer: ms, gRPC: gsrv, listener: lis, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_ = gsrv.Serve(lis)
	}()
	t.Cleanup(h.Stop)
	return h
}

// Stop halts the server. Safe to call multiple times.
func (h *ServerHarness) Stop() {
	h.gRPC.Stop()
	select {
	case <-h.done:
	case <-time.After(time.Second):
	}
}

// DialContext opens a bufconn client connection against the harness.
func (h *ServerHarness) DialContext(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	full := append([]grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return h.listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}, opts...)
	return grpc.NewClient("passthrough:bufnet", full...)
}

// Dialer returns a ContextDialer closure over the harness's bufconn listener.
// It is the right value to feed into grpc.WithContextDialer when a caller
// needs to plug in additional dial options themselves.
func (h *ServerHarness) Dialer() func(ctx context.Context, _ string) (net.Conn, error) {
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return h.listener.DialContext(ctx)
	}
}
