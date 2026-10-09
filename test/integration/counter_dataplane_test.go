//go:build integration && linux

package integration

import (
	"context"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/require"
)

func TestBMv2_CounterDataplane(t *testing.T) {
	p := counterValuePipeline(t)
	one, two := openEthernetPorts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, _ := counterValueReader(t, ctx, p)
	clear := func() {
		t.Helper()
		for _, tc := range counterUnits {
			require.NoError(t, r.Write(ctx, tc.name, 0, 0, 0))
		}
	}
	check := func(packets, bytes int64) {
		t.Helper()
		for _, tc := range counterUnits {
			samples, err := r.Read(ctx, tc.name, 0)
			require.NoError(t, err)
			require.Len(t, samples, 1)
			if tc.unit != p4configv1.CounterSpec_BYTES {
				require.Equal(t, packets, samples[0].Packets, tc.name)
			}
			if tc.unit != p4configv1.CounterSpec_PACKETS {
				require.Equal(t, bytes, samples[0].Bytes, tc.name)
			}
		}
	}
	marker := byte(130)
	send := func() int64 {
		t.Helper()
		marker++
		frame := l2Frame(marker)
		one.send(t, frame)
		require.True(t, two.receives(t, frame, 2*time.Second))
		return int64(len(frame))
	}
	clear()
	check(0, 0)
	var bytes int64
	for range 3 {
		bytes += send()
	}
	check(3, bytes)
	clear()
	check(0, 0)
	check(1, send())
}
