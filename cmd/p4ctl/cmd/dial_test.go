package cmd

import (
	"context"
	"net"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
)

func startDialServer(t *testing.T, opts ...grpc.ServerOption) (*testutil.MockServer, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(opts...)
	mock := testutil.NewMockServer()
	p4v1.RegisterP4RuntimeServer(server, mock)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		<-done
	})
	return mock, listener.Addr().String()
}

func setDialFlags(t *testing.T, flags globalFlags) {
	t.Helper()
	original := g
	g = flags
	t.Cleanup(func() { g = original })
}

func TestDialClient_TLSRejectsPlaintext(t *testing.T) {
	mock, addr := startDialServer(t)
	setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: false})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	c, err := dialClient(ctx)
	if c != nil {
		defer c.Close()
	}
	require.Error(t, err)
	require.Nil(t, c)
	mock.Mu.Lock()
	defer mock.Mu.Unlock()
	require.Zero(t, mock.ArbitrationEchoed, "a TLS connection must not arbitrate over plaintext")
}
