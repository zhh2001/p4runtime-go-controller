//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestBMv2_CLIConfig(t *testing.T) {
	bin := os.Getenv("P4RT_CLI_BIN")
	if bin == "" || deviceConfigPath() == "" {
		t.Skip("P4RT_CLI_BIN or P4RT_DEVICE_CONFIG unset; skipping live CLI configuration")
	}
	infoBytes, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	info := &p4configv1.P4Info{}
	require.NoError(t, prototext.Unmarshal(infoBytes, info))
	settings := filepath.Join(t.TempDir(), "controller.yaml")
	writeSettings := func(t *testing.T, values map[string]any) {
		t.Helper()
		data, err := yaml.Marshal(values)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(settings, data, 0o600))
	}
	writeSettings(t, map[string]any{"addr": targetAddr(), "device-id": 1, "election-id": 1, "insecure": true, "output": "yaml"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run := func(t *testing.T, env map[string]string, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, bin, args...)
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "P4CTL_") {
				command.Env = append(command.Env, entry)
			}
		}
		for key, value := range env {
			command.Env = append(command.Env, key+"="+value)
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		require.NoError(t, err, "CLI %v: %s", args, stderr.String())
		return output
	}
	decode := func(t *testing.T, format string, data []byte, value any) {
		t.Helper()
		if format == "yaml" {
			var document any
			require.NoError(t, yaml.Unmarshal(data, &document))
			var err error
			data, err = json.Marshal(document)
			require.NoError(t, err)
		}
		require.NoError(t, json.Unmarshal(data, value), "output: %s", data)
	}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			// File selection comes from the environment; the format flag wins.
			env := map[string]string{"P4CTL_CONFIG_FILE": settings, "P4CTL_OUTPUT": "json"}
			var connected struct {
				DeviceID   string `json:"device_id"`
				ElectionID string `json:"election_id"`
				Primary    bool   `json:"primary"`
			}
			decode(t, format, run(t, env, "connect", "--output", format), &connected)
			require.Equal(t, "1", connected.DeviceID)
			require.Equal(t, "0:1", connected.ElectionID)
			require.True(t, connected.Primary)

			var installed struct {
				Action    string   `json:"action"`
				Attempted []string `json:"attempted"`
			}
			decode(t, format, run(t, env, "--config-file", settings, "--output", format, "pipeline", "set",
				"--p4info", p4infoPath(), "--config", deviceConfigPath()), &installed)
			require.Contains(t, []string{"VERIFY_AND_COMMIT", "RECONCILE_AND_COMMIT"}, installed.Action)
			require.NotEmpty(t, installed.Attempted)
			require.Equal(t, installed.Action, installed.Attempted[len(installed.Attempted)-1])

			var returned json.RawMessage
			decode(t, format, run(t, env, "pipeline", "get", "--output", format), &returned)
			got := &p4configv1.P4Info{}
			require.NoError(t, protojson.Unmarshal(returned, got))
			require.Len(t, got.Tables, len(info.Tables))
			require.Equal(t, info.Tables[0].Preamble.Id, got.Tables[0].Preamble.Id)
			require.Equal(t, info.Tables[0].Preamble.Name, got.Tables[0].Preamble.Name)

			run(t, env, "table", "insert", "--p4info", p4infoPath(), "--table", "MyIngress.t_l2",
				"--match", "hdr.eth.dst=00:11:22:33:44:55", "--action", "MyIngress.forward", "--param", "port=2")
			var rows []json.RawMessage
			decode(t, format, run(t, env, "table", "read", "--output", format, "--p4info", p4infoPath(), "--table", "MyIngress.t_l2"), &rows)
			var ordinary []*p4v1.TableEntry
			for _, row := range rows {
				entry := &p4v1.TableEntry{}
				require.NoError(t, protojson.Unmarshal(row, entry))
				if !entry.IsDefaultAction {
					ordinary = append(ordinary, entry)
				}
			}
			require.Len(t, ordinary, 1)
			require.Equal(t, info.Tables[0].Preamble.Id, ordinary[0].TableId)
			require.Len(t, ordinary[0].Match, 1)
			require.Equal(t, []byte{0x11, 0x22, 0x33, 0x44, 0x55}, ordinary[0].Match[0].GetExact().Value)
			require.Len(t, ordinary[0].GetAction().GetAction().GetParams(), 1)
			require.Equal(t, []byte{2}, ordinary[0].GetAction().GetAction().Params[0].Value)
			run(t, env, "table", "delete", "--p4info", p4infoPath(), "--table", "MyIngress.t_l2", "--match", "hdr.eth.dst=00:11:22:33:44:55")

			var counters []map[string]any
			decode(t, format, run(t, env, "counter", "read", "--output", format, "--p4info", p4infoPath(), "--counter", "MyIngress.pkt_counter", "--index", "2"), &counters)
			require.Len(t, counters, 1)
			require.Equal(t, "MyIngress.pkt_counter", counters[0]["name"])
			require.Equal(t, "2", counters[0]["index"])
			require.Equal(t, "0", counters[0]["packets"])
			require.Equal(t, "0", counters[0]["bytes"])
		})
	}
	// Keep the legacy settings flag and the default text format usable.
	text := run(t, nil, "connect", "--config", settings, "--output", "table")
	require.Contains(t, string(text), "connected: device_id=1 election_id=0:1 state=primary primary=true")
	// Environment settings override the file, and flags override the environment.
	writeSettings(t, map[string]any{"addr": "127.0.0.1:1", "device-id": 2, "election-id": 3, "output": "yaml"})
	env := map[string]string{"P4CTL_CONFIG_FILE": settings, "P4CTL_ADDR": targetAddr(), "P4CTL_DEVICE_ID": "1", "P4CTL_ELECTION_ID": "1", "P4CTL_OUTPUT": "json"}
	require.True(t, json.Valid(run(t, env, "connect")))
	env["P4CTL_CONFIG_FILE"], env["P4CTL_ADDR"], env["P4CTL_DEVICE_ID"] = "missing.yaml", "127.0.0.1:1", "2"
	require.True(t, json.Valid(run(t, env, "connect", "--config-file", settings, "--addr", targetAddr(), "--device-id", "1")))
}
