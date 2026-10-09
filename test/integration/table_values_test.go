//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestBMv2_TableValues(t *testing.T) {
	bin, infoPath, configPath := os.Getenv("P4RT_CLI_BIN"), os.Getenv("P4RT_VALUE_P4INFO"), os.Getenv("P4RT_VALUE_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_VALUE_P4INFO and P4RT_VALUE_DEVICE_CONFIG unset; skipping live CLI literals")
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	settings := filepath.Join(t.TempDir(), "controller.yaml")
	require.NoError(t, os.WriteFile(settings, []byte("{}\n"), 0o600))
	run := func(t *testing.T, verb, table, match string, priority int32, value, port string) ([]byte, error) {
		t.Helper()
		args := []string{"--config-file", settings, "--addr", targetAddr(), "--device-id", "1", "--election-id", "1", "--role", "", "--insecure=true", "--output=json",
			"table", verb, "--p4info", infoPath, "--table", table}
		if verb != "read" {
			args = append(args, "--match", "m.key="+match, "--priority", fmt.Sprint(priority))
		}
		if verb == "insert" || verb == "modify" {
			args = append(args, "--action", "store", "--param", "value="+value, "--param", "port="+port)
		}
		command := exec.CommandContext(ctx, bin, args...)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "P4CTL_") {
				command.Env = append(command.Env, env)
			}
		}
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		if err != nil {
			return append(stdout.Bytes(), stderr.Bytes()...), err
		}
		return stdout.Bytes(), nil
	}
	read := func(t *testing.T, table string) []*p4v1.TableEntry {
		t.Helper()
		output, err := run(t, "read", table, "", 0, "", "")
		require.NoError(t, err, "read: %s", output)
		var rows []json.RawMessage
		require.NoError(t, json.Unmarshal(output, &rows))
		var entries []*p4v1.TableEntry
		for _, row := range rows {
			entry := &p4v1.TableEntry{}
			require.NoError(t, protojson.Unmarshal(row, entry))
			if !entry.IsDefaultAction {
				entries = append(entries, entry)
			}
		}
		return entries
	}
	for _, tc := range []struct {
		kind     string
		match    string
		priority int32
		key      string
		other    string
		value    string
		encoded  string
		port     string
	}{
		{"exact", "18446744073709551616", 0, "010000000000000000", "", "340282366920938463463374607431768211456", "0100000000000000000000000000000000", "511"},
		{"exact", "340282366920938463463374607431768211455", 0, "ffffffffffffffffffffffffffffffff", "", "680564733841876926926749214863536422911", "01ffffffffffffffffffffffffffffffff", "0x01:ff"},
		{"exact", "2001:db8::1", 0, "20010db8000000000000000000000001", "", "2001:db8::2", "20010db8000000000000000000000002", "01:ff"},
		{"exact", "01:02:03:04:05:06:07:08", 0, "010002000300040005000600070008", "", "0x01:02:03:04:05:06:07:08", "0102030405060708", "0X1FF"},
		{"exact", "00:12:34", 0, "1234", "", "192.0.2.1", "c0000201", "000511"},
		{"exact", "::ffff:192.0.2.1", 0, "ffffc0000201", "", "00:11:22:33:44:55", "1122334455", "511"},
		{"lpm", "2001:db8:1234::abcd/64", 0, "20010db8123400000000000000000000", "64", "0", "00", "511"},
		{"ternary", "2001:db8::1234&ffff:ffff:ffff:ffff::", 10, "20010db8000000000000000000000000", "ffffffffffffffff0000000000000000", "0x0001ff", "01ff", "511"},
		{"range", "18446744073709551616..18446744073709551617", 10, "010000000000000000", "010000000000000001", "1", "01", "511"},
		{"optional", "?340282366920938463463374607431768211455", 10, "ffffffffffffffffffffffffffffffff", "", "0", "00", "511"},
	} {
		t.Run(tc.kind+"/"+tc.match, func(t *testing.T) {
			table := "MyIngress.t_" + tc.kind
			output, err := run(t, "insert", table, tc.match, tc.priority, tc.value, tc.port)
			require.NoError(t, err, "insert: %s", output)
			entries := read(t, table)
			require.Len(t, entries, 1)
			entry := entries[0]
			require.Equal(t, tc.priority, entry.Priority)
			require.Len(t, entry.Match, 1)
			match := entry.Match[0]
			var value []byte
			switch tc.kind {
			case "exact":
				value = match.GetExact().Value
			case "lpm":
				value = match.GetLpm().Value
				require.Equal(t, tc.other, fmt.Sprint(match.GetLpm().PrefixLen))
			case "ternary":
				value = match.GetTernary().Value
				require.Equal(t, tc.other, hex.EncodeToString(match.GetTernary().Mask))
			case "range":
				value = match.GetRange().Low
				require.Equal(t, tc.other, hex.EncodeToString(match.GetRange().High))
			case "optional":
				value = match.GetOptional().Value
			}
			require.Equal(t, tc.key, hex.EncodeToString(value))
			params := entry.GetAction().GetAction().GetParams()
			require.Len(t, params, 2)
			require.Equal(t, tc.encoded, hex.EncodeToString(params[0].Value))
			require.Equal(t, []byte{1, 255}, params[1].Value)
			output, err = run(t, "modify", table, tc.match, tc.priority, "0", "0")
			require.NoError(t, err, "modify: %s", output)
			entries = read(t, table)
			require.Len(t, entries, 1)
			params = entries[0].GetAction().GetAction().GetParams()
			require.Len(t, params, 2)
			require.Equal(t, []byte{0}, params[0].Value)
			require.Equal(t, []byte{0}, params[1].Value)
			output, err = run(t, "delete", table, tc.match, tc.priority, "", "")
			require.NoError(t, err, "delete: %s", output)
			require.Empty(t, read(t, table))
		})
	}
	for _, tc := range []struct{ match, value, port, want string }{
		{"1", "x", "1", "param \"value\""}, {"1", "-1", "1", "param \"value\""},
		{"1", "", "1", "param \"value\""}, {"1", "0x", "1", "param \"value\""},
		{"1", "680564733841876926926749214863536422912", "1", "129-bit"},
		{"1", "0", "x", "param \"port\""}, {"1", "0", "512", "9-bit"},
		{"340282366920938463463374607431768211456", "0", "1", "128-bit"},
		{"2001:db8::g", "0", "1", "match \"m.key\""},
	} {
		for _, verb := range []string{"insert", "modify"} {
			output, err := run(t, verb, "MyIngress.t_exact", tc.match, 0, tc.value, tc.port)
			require.Error(t, err, "accepted %s: %s", verb, output)
			require.Contains(t, string(output), tc.want)
			require.NotContains(t, string(output), "panic:")
		}
	}
	require.Empty(t, read(t, "MyIngress.t_exact"))
}
