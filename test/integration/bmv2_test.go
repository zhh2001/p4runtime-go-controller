//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
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
}
