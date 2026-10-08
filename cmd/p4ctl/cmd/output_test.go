package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("P4CTL_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			rootCmd.SetArgs(os.Args[i+1:])
			if err := Execute(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI argument separator")
}

func runCLIProcess(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	bin, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bin, append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "P4CTL_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "P4CTL_TEST_PROCESS=1")
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	return stdout.String(), stderr.String(), err
}

func cliSettings(t *testing.T, values map[string]any) string {
	t.Helper()
	data, err := yaml.Marshal(values)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func outputJSON(t *testing.T, format, output string) []byte {
	t.Helper()
	if format == "json" {
		require.True(t, json.Valid([]byte(output)), "not a complete JSON document: %s", output)
		return []byte(output)
	}
	var value any
	require.NoError(t, yaml.Unmarshal([]byte(output), &value))
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestCLI_StructuredOutput(t *testing.T) {
	path := tableFixture(t)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	info := &p4configv1.P4Info{}
	require.NoError(t, prototext.Unmarshal(data, info))
	info.Counters = []*p4configv1.Counter{{Preamble: &p4configv1.Preamble{Id: 0x12000001, Name: "ingress.count"}, Size: 2}}
	require.NoError(t, os.WriteFile(path, []byte(prototext.Format(info)), 0o600))
	entry := &p4v1.TableEntry{TableId: 0x02000001, IdleTimeoutNs: 9223372036854775807, Metadata: []byte{0, 255},
		Match: []*p4v1.FieldMatch{{FieldId: 1, FieldMatchType: &p4v1.FieldMatch_Exact_{Exact: &p4v1.FieldMatch_Exact{Value: []byte{0}}}}},
		Action: &p4v1.TableAction{Type: &p4v1.TableAction_Action{Action: &p4v1.Action{ActionId: 0x01000001,
			Params: []*p4v1.Action_Param{{ParamId: 1, Value: []byte{255}}}}}}}
	for _, format := range []string{"json", "yaml"} {
		for _, verb := range []string{"connect", "pipeline set", "pipeline get", "table read", "counter read", "empty table", "empty counter"} {
			t.Run(format+"/"+verb, func(t *testing.T) {
				mock, addr := startDialServer(t)
				mock.Mu.Lock()
				mock.GetPipelineResp = &p4v1.GetForwardingPipelineConfigResponse{Config: &p4v1.ForwardingPipelineConfig{P4Info: info}}
				if !strings.HasPrefix(verb, "empty") {
					mock.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{
						{Entity: &p4v1.Entity_TableEntry{TableEntry: entry}},
						{Entity: &p4v1.Entity_CounterEntry{CounterEntry: &p4v1.CounterEntry{CounterId: 0x12000001,
							Index: &p4v1.Index{Index: 1}, Data: &p4v1.CounterData{PacketCount: 9223372036854775807, ByteCount: 9007199254740993}}}},
					}}}
				}
				mock.SetPipelineErrByAction = map[p4v1.SetForwardingPipelineConfigRequest_Action]error{
					p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT: status.Error(codes.Unimplemented, "unsupported action"),
				}
				mock.Mu.Unlock()
				settings := cliSettings(t, map[string]any{"addr": addr, "device-id": "18446744073709551615", "election-id": "9007199254740993", "output": format})
				args := []string{"--config-file", settings}
				switch verb {
				case "connect":
					args = append(args, "connect")
				case "pipeline set", "pipeline get":
					args = append(args, strings.Fields(verb)...)
					if verb == "pipeline set" {
						args = append(args, "--p4info", path)
					}
				case "table read", "empty table":
					args = append(args, "table", "read", "--p4info", path, "--table", "ingress.t_EXACT")
				case "counter read", "empty counter":
					args = append(args, "counter", "read", "--p4info", path, "--counter", "ingress.count")
				}
				stdout, stderr, err := runCLIProcess(t, nil, args...)
				require.NoError(t, err, "stderr: %s", stderr)
				encoded := outputJSON(t, format, stdout)
				switch verb {
				case "connect":
					var result map[string]any
					require.NoError(t, json.Unmarshal(encoded, &result))
					assert.Equal(t, "18446744073709551615", result["device_id"])
					assert.Equal(t, "0:9007199254740993", result["election_id"])
					assert.Equal(t, "primary", result["state"])
					assert.Equal(t, true, result["primary"])
				case "pipeline set":
					var result map[string]any
					require.NoError(t, json.Unmarshal(encoded, &result))
					assert.Equal(t, "RECONCILE_AND_COMMIT", result["action"])
					assert.Equal(t, []any{"VERIFY_AND_COMMIT", "RECONCILE_AND_COMMIT"}, result["attempted"])
				case "pipeline get":
					got := &p4configv1.P4Info{}
					require.NoError(t, protojson.Unmarshal(encoded, got))
					assert.True(t, proto.Equal(info, got))
				case "table read":
					var rows []json.RawMessage
					require.NoError(t, json.Unmarshal(encoded, &rows))
					require.Len(t, rows, 1)
					got := &p4v1.TableEntry{}
					require.NoError(t, protojson.Unmarshal(rows[0], got))
					assert.True(t, proto.Equal(entry, got), "table fields lost during output: %v", got)
				case "counter read":
					var rows []map[string]any
					require.NoError(t, json.Unmarshal(encoded, &rows))
					require.Len(t, rows, 1)
					assert.Equal(t, "ingress.count", rows[0]["name"])
					assert.Equal(t, float64(0x12000001), rows[0]["id"])
					assert.Equal(t, "1", rows[0]["index"])
					assert.Equal(t, "9223372036854775807", rows[0]["packets"])
					assert.Equal(t, "9007199254740993", rows[0]["bytes"])
				default:
					assert.JSONEq(t, "[]", string(encoded))
				}
			})
		}
	}
}

func TestCLI_ConfigFlagWithDeviceConfig(t *testing.T) {
	mock, addr := startDialServer(t)
	settings := cliSettings(t, map[string]any{"addr": addr, "output": "json"})
	device := filepath.Join(t.TempDir(), "device.json")
	require.NoError(t, os.WriteFile(device, []byte{0xde, 0xad}, 0o600))
	stdout, stderr, err := runCLIProcess(t, map[string]string{"P4CTL_CONFIG_FILE": "missing.yaml"},
		"--config-file", settings, "pipeline", "set", "--p4info", tableFixture(t), "--config", device)
	require.NoError(t, err, "stderr: %s", stderr)
	require.JSONEq(t, `{"action":"VERIFY_AND_COMMIT","attempted":["VERIFY_AND_COMMIT"]}`, stdout)
	mock.Mu.Lock()
	defer mock.Mu.Unlock()
	require.NotNil(t, mock.SetPipelineReq)
	assert.Equal(t, []byte{0xde, 0xad}, mock.SetPipelineReq.GetConfig().GetP4DeviceConfig())
}

func TestCLI_ConfigTransport(t *testing.T) {
	ca := newTLSCA(t)
	server, controller := ca.identity(t, false, false), ca.identity(t, true, false)
	mock, addr := startDialServer(t, grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.pool,
	})))
	settings := cliSettings(t, map[string]any{"addr": addr, "insecure": false, "tls-ca": ca.file, "tls-server-name": "switch.test", "output": "yaml"})
	stdout, stderr, err := runCLIProcess(t, map[string]string{
		"P4CTL_TLS_CERT": controller.certFile, "P4CTL_TLS_KEY": controller.keyFile, "P4CTL_OUTPUT": "json",
	}, "--config-file", settings, "connect")
	require.NoError(t, err, "stderr: %s", stderr)
	require.True(t, json.Valid([]byte(stdout)))
	mock.Mu.Lock()
	defer mock.Mu.Unlock()
	assert.Equal(t, 1, mock.ArbitrationEchoed)
}

func TestCLI_ConfigErrorBeforeDial(t *testing.T) {
	mock, addr := startDialServer(t)
	settings := cliSettings(t, map[string]any{"addr": addr})
	stdout, stderr, err := runCLIProcess(t, map[string]string{"P4CTL_OUTPUT": "xml"}, "--config-file", settings, "connect")
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "unknown output format")
	mock.Mu.Lock()
	defer mock.Mu.Unlock()
	assert.Zero(t, mock.ArbitrationEchoed)
}

func TestCLI_VersionRemainsText(t *testing.T) {
	settings := cliSettings(t, nil)
	stdout, stderr, err := runCLIProcess(t, nil, "--config-file", settings, "--output", "json", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "dev\n", stdout)
}

type outputErrorWriter struct{ err error }

func (w outputErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStructuredOutput_WriteError(t *testing.T) {
	setDialFlags(t, globalFlags{Output: "json"})
	command := &cobra.Command{}
	err := errors.New("output unavailable")
	command.SetOut(outputErrorWriter{err})
	require.ErrorIs(t, writeStructuredOutput(command, map[string]string{"state": "primary"}), err)
}
