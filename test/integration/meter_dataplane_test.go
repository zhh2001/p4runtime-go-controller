//go:build integration && linux

package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/meter"
)

func TestBMv2_MeterDataplane(t *testing.T) {
	p := meterPipeline(t)
	one, two := openEthernetPorts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	r, err := meter.NewReader(c, p)
	require.NoError(t, err)
	marker := byte(110)
	check := func(colors ...byte) {
		t.Helper()
		for _, color := range colors {
			marker++
			frame := l2Frame(marker)
			want := bytes.Clone(frame)
			want[11] = color
			one.send(t, frame)
			require.True(t, two.receives(t, want, 2*time.Second), "expected color %d for marker %d", color, marker)
		}
	}
	// With zero rates, tokens cannot refill between packets.
	check(0, 0, 0)
	require.NoError(t, r.Write(ctx, "rate", 0, meter.Config{CBurst: 1, PBurst: 2}))
	check(0, 1, 2)
	require.NoError(t, r.Write(ctx, "rate", 0, meter.Config{}))
	check(2, 2, 2)
	require.NoError(t, r.Reset(ctx, "rate", 0))
	check(0, 0, 0)
	entries, err := r.Read(ctx, "rate", 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Nil(t, entries[0].Config)
	require.NoError(t, r.Write(ctx, "rate", 0, meter.Config{CBurst: 2, PBurst: 1}))
	check(0, 2, 2)
	require.NoError(t, r.Reset(ctx, "rate", 0))
	check(0, 0, 0)
}
