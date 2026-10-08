//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func TestBMv2_TableCLI(t *testing.T) {
	bin, infoPath, configPath := os.Getenv("P4RT_CLI_BIN"), os.Getenv("P4RT_KEY_P4INFO"), os.Getenv("P4RT_KEY_DEVICE_CONFIG")
	if bin == "" && infoPath == "" && configPath == "" {
		t.Skip("P4RT_CLI_BIN, P4RT_KEY_P4INFO and P4RT_KEY_DEVICE_CONFIG unset; skipping live CLI tests")
	}
	require.NotEmpty(t, bin)
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	settings := filepath.Join(t.TempDir(), "cli.yaml")
	require.NoError(t, os.WriteFile(settings, []byte("{}\n"), 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	connect := func(t *testing.T) *client.Client {
		t.Helper()
		c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
		require.NoError(t, err)
		if err := c.BecomePrimary(ctx); err != nil {
			c.Close()
			t.Fatal(err)
		}
		return c
	}
	c := connect(t)
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	c.Close()
	require.NoError(t, err)
	read := func(t *testing.T, tableID uint32) []*p4v1.TableEntry {
		t.Helper()
		c := connect(t)
		defer c.Close()
		entries, err := c.ReadTableEntries(ctx, tableID)
		require.NoError(t, err)
		var ordinary []*p4v1.TableEntry
		for _, entry := range entries {
			if !entry.IsDefaultAction {
				ordinary = append(ordinary, entry)
			}
		}
		return ordinary
	}
	run := func(t *testing.T, verb, table string, matches []string, priority int32, port string, want string) {
		t.Helper()
		args := []string{"table", verb, "--addr", targetAddr(), "--device-id", "1", "--election-id", "1", "--role", "", "--insecure=true", "--config", settings, "--table", table, "--p4info", infoPath, "--priority", fmt.Sprint(priority)}
		for _, match := range matches {
			args = append(args, "--match", match)
		}
		if port != "" {
			args = append(args, "--action", "MyIngress.forward", "--param", "port="+port)
		}
		command := exec.CommandContext(ctx, bin, args...)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "P4CTL_") {
				command.Env = append(command.Env, env)
			}
		}
		output, err := command.CombinedOutput()
		if want != "" {
			require.Error(t, err, "command unexpectedly succeeded: %s", output)
			require.Contains(t, string(output), want)
		} else {
			require.NoError(t, err, "CLI output: %s", output)
		}
	}
	keyOf := func(entry *p4v1.TableEntry) *p4v1.TableEntry {
		return &p4v1.TableEntry{TableId: entry.TableId, Match: entry.Match, Priority: entry.Priority, IsDefaultAction: entry.IsDefaultAction}
	}
	for _, tc := range []struct {
		name          string
		table         string
		matches       []string
		keeperMatches []string
		priority      int32
		keeperPrio    int32
		matchCount    int
	}{
		{"exact zero", "t_exact", []string{"s.ingress_port=0"}, []string{"s.ingress_port=1"}, 0, 0, 1},
		{"LPM masked suffix", "t_lpm", []string{"s.packet_length=10.9.8.7/8"}, []string{"s.packet_length=11.0.0.0/8"}, 0, 0, 1},
		{"LPM wildcard", "t_lpm", []string{"s.packet_length=0/0"}, []string{"s.packet_length=11.0.0.0/8"}, 0, 0, 0},
		{"ternary canonical mask", "t_ternary", []string{"s.ingress_port=0x01ff&0x00ff"}, []string{"s.ingress_port=0xfe&0xff"}, 10, 10, 1},
		{"same match different priority", "t_ternary", []string{"s.ingress_port=1&511"}, []string{"s.ingress_port=1&511"}, 10, 20, 1},
		{"ternary wildcard", "t_ternary", []string{"s.ingress_port=0&0"}, []string{"s.ingress_port=1&511"}, 10, 20, 0},
		{"range", "t_range", []string{"s.ingress_port=1..3"}, []string{"s.ingress_port=4..6"}, 10, 10, 1},
		{"range wildcard", "t_range", nil, []string{"s.ingress_port=4..6"}, 10, 10, 0},
		{"range full domain", "t_range", []string{"s.ingress_port=0..511"}, []string{"s.ingress_port=4..6"}, 10, 10, 0},
		{"optional zero", "t_optional", []string{"s.ingress_port=?0"}, []string{"s.ingress_port=?1"}, 10, 10, 1},
		{"optional wildcard", "t_optional", nil, []string{"s.ingress_port=?1"}, 10, 10, 0},
		{"mixed wildcard", "t_mixed", []string{"s.ingress_port=0", "s.egress_spec=0&0"}, []string{"s.ingress_port=1"}, 10, 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := "MyIngress." + tc.table
			td, ok := p.Table(table)
			require.True(t, ok)
			run(t, "insert", table, tc.matches, tc.priority, "1", "")
			run(t, "insert", table, tc.keeperMatches, tc.keeperPrio, "2", "")
			entries := read(t, td.ID)
			require.Len(t, entries, 2)
			var target, keeper *p4v1.TableEntry
			for _, entry := range entries {
				action := entry.GetAction().GetAction()
				require.Len(t, action.GetParams(), 1)
				if string(action.Params[0].Value) == string([]byte{1}) {
					target = keyOf(entry)
				} else {
					require.Equal(t, []byte{2}, action.Params[0].Value)
					keeper = keyOf(entry)
				}
			}
			require.NotNil(t, target)
			require.NotNil(t, keeper)
			require.Equal(t, tc.priority, target.Priority)
			if (tc.name == "range wildcard" || tc.name == "range full domain") && len(target.Match) == 1 {
				// Some PI versions read back a wildcard as an explicit full range.
				require.Equal(t, []byte{0}, target.Match[0].GetRange().GetLow())
				require.Equal(t, []byte{1, 255}, target.Match[0].GetRange().GetHigh())
			} else {
				require.Len(t, target.Match, tc.matchCount)
			}
			if tc.name == "same match different priority" {
				run(t, "delete", table, tc.matches, 30, "", "NotFound")
				require.Len(t, read(t, td.ID), 2)
			}
			run(t, "modify", table, tc.matches, tc.priority, "3", "")
			entries = read(t, td.ID)
			require.Len(t, entries, 2)
			for _, entry := range entries {
				port := byte(2)
				if proto.Equal(target, keyOf(entry)) {
					port = 3
				} else {
					require.True(t, proto.Equal(keeper, keyOf(entry)))
				}
				require.Equal(t, []byte{port}, entry.GetAction().GetAction().GetParams()[0].Value)
			}
			run(t, "delete", table, tc.matches, tc.priority, "", "")
			entries = read(t, td.ID)
			require.Len(t, entries, 1)
			require.True(t, proto.Equal(keeper, keyOf(entries[0])), "the other entry changed after deletion")
			require.Equal(t, []byte{2}, entries[0].GetAction().GetAction().GetParams()[0].Value)
			run(t, "delete", table, tc.matches, tc.priority, "", "NotFound")
			require.Len(t, read(t, td.ID), 1)
			run(t, "delete", table, tc.keeperMatches, tc.keeperPrio, "", "")
			require.Empty(t, read(t, td.ID))
		})
	}
	run(t, "delete", "MyIngress.t_exact", nil, 0, "", "exact field")
	run(t, "delete", "MyIngress.t_exact", []string{"s.ingress_port=1"}, 1, "", "zero priority")
	run(t, "delete", "MyIngress.t_ternary", nil, 0, "", "positive priority")
	run(t, "delete", "MyIngress.t_optional", nil, -1, "", "positive priority")
}
