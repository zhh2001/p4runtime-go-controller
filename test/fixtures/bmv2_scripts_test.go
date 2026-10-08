package fixtures_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scriptFixture struct {
	root string
	bin  string
	log  string
	env  map[string]string
}

func newScriptFixture(t *testing.T) *scriptFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("BMv2 scripts require a Unix shell")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	f := &scriptFixture{root: filepath.Join(t.TempDir(), "project with spaces")}
	f.bin, f.log = filepath.Join(f.root, "bin"), filepath.Join(f.root, "calls")
	f.env = map[string]string{
		"PATH":               f.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CAPTURE":            f.log,
		"P4RT_P4INFO":        "",
		"P4RT_DEVICE_CONFIG": "",
		"P4RT_TARGET":        "",
		"P4C_BM2_SS":         filepath.Join(f.bin, "compiler"),
		"SIMPLE_SWITCH_GRPC": filepath.Join(f.bin, "switch"),
		"GO":                 filepath.Join(f.bin, "go"),
	}
	for _, name := range []string{"run-bmv2.sh", "test-bmv2.sh", "compile-l2.sh"} {
		data, err := os.ReadFile(filepath.Join("../../scripts", name))
		require.NoError(t, err)
		f.write(t, filepath.Join("scripts", name), string(data))
	}
	f.write(t, "bin/compiler", `#!/usr/bin/env bash
echo compile >> "$CAPTURE"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --p4runtime-files) INFO="$2"; shift 2 ;;
    -o) CONFIG="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo info > "$INFO"
echo config > "$CONFIG"
`)
	return f
}

func (f *scriptFixture) write(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(f.root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o755))
	return path
}

func (f *scriptFixture) run(t *testing.T, name string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{filepath.Join(f.root, "scripts", name)}, args...)...)
	cmd.Dir = f.root
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, overridden := f.env[key]; !overridden {
			cmd.Env = append(cmd.Env, item)
		}
	}
	for key, value := range f.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "script timed out: %s", output)
	if err == nil {
		return string(output), 0
	}
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "%s", output)
	return string(output), exit.ExitCode()
}

func (f *scriptFixture) calls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func TestBMv2LauncherContainerOwnership(t *testing.T) {
	for _, name := range []string{"existing container", "start failure", "exited container"} {
		t.Run(name, func(t *testing.T) {
			f := newScriptFixture(t)
			f.env["EXISTING"] = "false"
			f.env["START_FAIL"] = "false"
			if name == "existing container" {
				f.env["EXISTING"] = "true"
			}
			if name == "start failure" {
				f.env["START_FAIL"] = "true"
			}
			f.write(t, "bin/docker", `#!/usr/bin/env bash
echo "$*" >> "$CAPTURE"
case "$1 $2" in
  'info ') exit 0 ;;
  'container inspect')
    if [[ "$3" == --format ]]; then echo false; exit 0; fi
    [[ "$EXISTING" == true ]]; exit $? ;;
  'create --name') echo new-container-id ;;
  'start new-container-id') [[ "$START_FAIL" == false ]]; exit $? ;;
  'logs new-container-id'|'rm -f') exit 0 ;;
  *) exit 99 ;;
esac
`)
			output, code := f.run(t, "run-bmv2.sh", "--docker", "--name", "existing-name", "--output", filepath.Join(f.root, "artifacts"))
			calls := f.calls(t)
			if name == "existing container" {
				require.Equal(t, 2, code, "%s", output)
				require.Contains(t, output, "already exists")
				require.Equal(t, "info\ncontainer inspect existing-name\n", calls)
			} else {
				require.Equal(t, 1, code, "%s", output)
				if name == "exited container" {
					require.Contains(t, output, "exited during startup")
				}
				require.Contains(t, calls, "logs new-container-id\nrm -f new-container-id\n")
				require.NotContains(t, calls, "rm -f existing-name")
			}
		})
	}
}

func TestBMv2LauncherCompilationFailure(t *testing.T) {
	f := newScriptFixture(t)
	f.write(t, "bin/compiler", "#!/usr/bin/env bash\necho compile >> \"$CAPTURE\"\nexit 42\n")
	f.write(t, "bin/switch", "#!/usr/bin/env bash\necho switch >> \"$CAPTURE\"\nexit 0\n")
	output, code := f.run(t, "run-bmv2.sh", "--output", filepath.Join(f.root, "artifacts"))
	require.Equal(t, 42, code, "%s", output)
	require.Equal(t, "compile\n", f.calls(t))
	files, err := filepath.Glob(filepath.Join(f.root, "artifacts/.l2-build.*"))
	require.NoError(t, err)
	require.Empty(t, files, "compiler staging files must be removed")
}

func TestBMv2LauncherNativeForeground(t *testing.T) {
	for _, interfaces := range [][]string{nil, {"1@host1"}, {"1@host1", "2@host2"}} {
		t.Run(strings.Join(interfaces, ","), func(t *testing.T) {
			f := newScriptFixture(t)
			f.write(t, "bin/switch", `#!/usr/bin/env bash
printf '%s\n' "$@" >> "$CAPTURE"
exit 43
`)
			args := []string{"-p10559", "-t10090", "--output", filepath.Join(f.root, "artifacts")}
			bindings := ""
			for _, binding := range interfaces {
				args = append(args, "--interface", binding)
				bindings += "-i\n" + binding + "\n"
			}
			output, code := f.run(t, "run-bmv2.sh", args...)
			require.Equal(t, 43, code, "the foreground target's status must reach the caller: %s", output)
			require.Contains(t, f.calls(t), "--log-console\n"+bindings+"--notifications-addr\n")
			require.Contains(t, f.calls(t), "--grpc-server-addr\n127.0.0.1:10559\n")
		})
	}
}

func TestBMv2TestScriptPipelinePaths(t *testing.T) {
	for _, name := range []string{"compile", "relative pair", "symlinked project", "missing pair", "empty file"} {
		t.Run(name, func(t *testing.T) {
			f := newScriptFixture(t)
			if name == "symlinked project" {
				link := filepath.Join(t.TempDir(), "linked project")
				require.NoError(t, os.Symlink(f.root, link))
				f.root = link
			}
			f.write(t, "bin/go", `#!/usr/bin/env bash
printf '%s\n' "$P4RT_P4INFO" "$P4RT_DEVICE_CONFIG" "$P4RT_TARGET" "$PWD" "$@" >> "$CAPTURE"
exit 37
`)
			expectedInfo := filepath.Join(f.root, "build/integration/l2/l2.p4info.txt")
			expectedConfig := filepath.Join(f.root, "build/integration/l2/l2.bmv2.json")
			if name != "compile" {
				f.write(t, "custom pair/info.txt", "info")
				f.write(t, "custom pair/config.json", "config")
				f.env["P4RT_P4INFO"] = "custom pair/info.txt"
				expectedInfo = filepath.Join(f.root, f.env["P4RT_P4INFO"])
				if name != "missing pair" {
					f.env["P4RT_DEVICE_CONFIG"] = "custom pair/config.json"
					expectedConfig = filepath.Join(f.root, f.env["P4RT_DEVICE_CONFIG"])
				}
				if name == "empty file" {
					f.write(t, "custom pair/config.json", "")
				}
			}
			f.env["P4RT_TARGET"] = "127.0.0.1:10559"
			output, code := f.run(t, "test-bmv2.sh", "-run", "TestBMv2_PacketCPUPort")
			calls := f.calls(t)
			switch name {
			case "missing pair":
				require.Equal(t, 2, code, "%s", output)
				require.Contains(t, output, "together")
				require.Empty(t, calls)
			case "empty file":
				require.Equal(t, 1, code, "%s", output)
				require.Contains(t, output, "missing or empty")
				require.Empty(t, calls)
			default:
				require.Equal(t, 37, code, "Go test's status must reach the caller: %s", output)
				lines := strings.Split(strings.TrimSuffix(calls, "\n"), "\n")
				if name == "compile" {
					require.Equal(t, "compile", lines[0])
					lines = lines[1:]
				}
				require.Len(t, lines, 11)
				requireSamePath(t, expectedInfo, lines[0])
				requireSamePath(t, expectedConfig, lines[1])
				require.Equal(t, "127.0.0.1:10559", lines[2])
				requireSamePath(t, f.root, lines[3])
				require.Equal(t, []string{"test", "-race", "-tags=integration", "-count=1", "-run", "TestBMv2_PacketCPUPort", "./test/integration/..."}, lines[4:])
			}
		})
	}
}

func requireSamePath(t *testing.T, expected, actual string) {
	t.Helper()
	require.True(t, filepath.IsAbs(actual), "path must be absolute: %s", actual)
	want, err := os.Stat(expected)
	require.NoError(t, err)
	got, err := os.Stat(actual)
	require.NoError(t, err)
	require.True(t, os.SameFile(want, got), "%s and %s must identify the same file", expected, actual)
}
