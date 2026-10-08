package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetRootFlags(t *testing.T) {
	t.Helper()
	original := g
	changed := map[string]bool{}
	rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		changed[flag.Name] = flag.Changed
		require.NoError(t, flag.Value.Set(flag.DefValue))
		flag.Changed = false
		if flag.Name != "config" && flag.Name != "config-file" {
			t.Setenv("P4CTL_"+envKey(flag.Name), "")
		}
	})
	t.Setenv("P4CTL_CONFIG_FILE", "")
	t.Cleanup(func() {
		g = original
		rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { flag.Changed = changed[flag.Name] })
	})
}

func envKey(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func TestInitConfig_EnvWithoutConfig(t *testing.T) {
	resetRootFlags(t)
	t.Setenv("P4CTL_ADDR", "env.test:1234")
	t.Setenv("P4CTL_DEVICE_ID", "7")
	t.Setenv("P4CTL_ELECTION_ID", "9")
	t.Setenv("P4CTL_ROLE", "env-role")
	t.Setenv("P4CTL_INSECURE", "false")
	t.Setenv("P4CTL_OUTPUT", "json")
	flags, err := loadGlobalFlags(rootCmd.PersistentFlags(), t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "env.test:1234", flags.Addr)
	assert.EqualValues(t, 7, flags.DeviceID)
	assert.EqualValues(t, 9, flags.Election)
	assert.Equal(t, "env-role", flags.Role)
	assert.False(t, flags.Insecure)
	assert.Equal(t, "json", flags.Output)
}

func TestGlobalFlags_Precedence(t *testing.T) {
	for _, source := range []string{"file", "environment", "flags"} {
		t.Run(source, func(t *testing.T) {
			resetRootFlags(t)
			file := filepath.Join(t.TempDir(), "settings.yaml")
			require.NoError(t, os.WriteFile(file, []byte("addr: file.test:9559\ndevice-id: 7\nelection-id: 9\nrole: file-role\ninsecure: false\ntls-ca: file-ca\ntls-server-name: file-name\ntls-cert: file-cert\ntls-key: file-key\noutput: yaml\n"), 0o600))
			flags := rootCmd.PersistentFlags()
			require.NoError(t, flags.Set("config-file", file))
			want := globalFlags{Addr: "file.test:9559", DeviceID: 7, Election: 9, Role: "file-role", Insecure: false,
				TLSCA: "file-ca", TLSServerName: "file-name", TLSCert: "file-cert", TLSKey: "file-key", Output: "yaml", ConfigFile: file}
			if source != "file" {
				for name, value := range map[string]string{"addr": "env.test:9559", "device-id": "11", "election-id": "13", "role": "env-role", "insecure": "true",
					"tls-ca": "env-ca", "tls-server-name": "env-name", "tls-cert": "env-cert", "tls-key": "env-key", "output": "json"} {
					t.Setenv("P4CTL_"+envKey(name), value)
				}
				want = globalFlags{Addr: "env.test:9559", DeviceID: 11, Election: 13, Role: "env-role", Insecure: true,
					TLSCA: "env-ca", TLSServerName: "env-name", TLSCert: "env-cert", TLSKey: "env-key", Output: "json", ConfigFile: file}
			}
			if source == "flags" {
				for name, value := range map[string]string{"addr": "flag.test:9559", "device-id": "17", "election-id": "19", "role": "", "insecure": "false",
					"tls-ca": "", "tls-server-name": "flag-name", "tls-cert": "flag-cert", "tls-key": "flag-key", "output": "table"} {
					require.NoError(t, flags.Set(name, value))
				}
				want = globalFlags{Addr: "flag.test:9559", DeviceID: 17, Election: 19, Insecure: false,
					TLSServerName: "flag-name", TLSCert: "flag-cert", TLSKey: "flag-key", Output: "table", ConfigFile: file}
			}
			got, err := loadGlobalFlags(flags, "")
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestGlobalFlags_DefaultConfig(t *testing.T) {
	resetRootFlags(t)
	home := t.TempDir()
	got, err := loadGlobalFlags(rootCmd.PersistentFlags(), home)
	require.NoError(t, err)
	assert.Equal(t, globalFlags{Addr: "127.0.0.1:9559", DeviceID: 1, Election: 1, Insecure: true, Output: "table", ConfigFile: filepath.Join(home, ".p4ctl.yaml")}, got)
	require.NoError(t, os.WriteFile(got.ConfigFile, []byte("addr: default-file.test:9559\noutput: json\n"), 0o600))
	got, err = loadGlobalFlags(rootCmd.PersistentFlags(), home)
	require.NoError(t, err)
	assert.Equal(t, "default-file.test:9559", got.Addr)
	assert.Equal(t, "json", got.Output)
	require.NoError(t, os.WriteFile(got.ConfigFile, []byte("addr: [\n"), 0o600))
	_, err = loadGlobalFlags(rootCmd.PersistentFlags(), home)
	require.ErrorContains(t, err, "read CLI config")
}

func TestGlobalFlags_ConfigSelection(t *testing.T) {
	for _, selector := range []string{"config-file", "config", "environment", "both", "empty"} {
		t.Run(selector, func(t *testing.T) {
			resetRootFlags(t)
			file := filepath.Join(t.TempDir(), "settings.yaml")
			require.NoError(t, os.WriteFile(file, []byte("addr: selected.test:9559\n"), 0o600))
			flags := rootCmd.PersistentFlags()
			t.Setenv("P4CTL_CONFIG_FILE", file)
			switch selector {
			case "config-file", "config":
				t.Setenv("P4CTL_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
				require.NoError(t, flags.Set(selector, file))
			case "both":
				require.NoError(t, flags.Set("config", file))
				require.NoError(t, flags.Set("config-file", file))
			case "empty":
				require.NoError(t, flags.Set("config-file", ""))
			}
			got, err := loadGlobalFlags(flags, "")
			if selector == "both" || selector == "empty" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, file, got.ConfigFile)
			assert.Equal(t, "selected.test:9559", got.Addr)
		})
	}
}

func TestGlobalFlags_InvalidValues(t *testing.T) {
	for _, setting := range []struct {
		key   string
		value string
	}{
		{"device-id", "-1"}, {"device-id", "1.5"}, {"device-id", "18446744073709551616"}, {"election-id", "bad"},
		{"insecure", "bad"}, {"role", "7"}, {"tls-ca", "[one, two]"}, {"output", "xml"},
	} {
		t.Run(setting.key+"="+setting.value, func(t *testing.T) {
			resetRootFlags(t)
			file := filepath.Join(t.TempDir(), "settings.yaml")
			require.NoError(t, os.WriteFile(file, []byte(setting.key+": "+setting.value+"\n"), 0o600))
			require.NoError(t, rootCmd.PersistentFlags().Set("config-file", file))
			_, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
			require.ErrorContains(t, err, setting.key)
		})
	}
	for _, key := range []string{"device-id", "election-id", "insecure", "output"} {
		t.Run("environment "+key, func(t *testing.T) {
			resetRootFlags(t)
			t.Setenv("P4CTL_"+envKey(key), "invalid")
			_, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
			require.ErrorContains(t, err, key)
		})
	}
}

func TestGlobalFlags_IntegerPrecision(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			resetRootFlags(t)
			data := "device-id: 18446744073709551615\nelection-id: 9007199254740993\n"
			if format == "json" {
				data = `{"device-id":18446744073709551615,"election-id":9007199254740993}`
			}
			file := filepath.Join(t.TempDir(), "settings."+format)
			require.NoError(t, os.WriteFile(file, []byte(data), 0o600))
			require.NoError(t, rootCmd.PersistentFlags().Set("config-file", file))
			got, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
			require.NoError(t, err)
			assert.Equal(t, ^uint64(0), got.DeviceID)
			assert.Equal(t, uint64(9007199254740993), got.Election)
		})
	}
}

func TestGlobalFlags_JSONErrors(t *testing.T) {
	for i, data := range []string{`{"addr":`, `{} {}`, `{} trailing`} {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			resetRootFlags(t)
			file := filepath.Join(t.TempDir(), "settings.json")
			require.NoError(t, os.WriteFile(file, []byte(data), 0o600))
			require.NoError(t, rootCmd.PersistentFlags().Set("config-file", file))
			_, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
			require.ErrorContains(t, err, "read CLI config")
		})
	}
}

func TestGlobalFlags_OverriddenInvalidValue(t *testing.T) {
	resetRootFlags(t)
	file := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(file, []byte("device-id: -1\ninsecure: broken\n"), 0o600))
	require.NoError(t, rootCmd.PersistentFlags().Set("config-file", file))
	t.Setenv("P4CTL_DEVICE_ID", "23")
	t.Setenv("P4CTL_INSECURE", "false")
	got, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
	require.NoError(t, err)
	assert.EqualValues(t, 23, got.DeviceID)
	assert.False(t, got.Insecure)
	t.Setenv("P4CTL_DEVICE_ID", "bad")
	require.NoError(t, rootCmd.PersistentFlags().Set("device-id", "29"))
	got, err = loadGlobalFlags(rootCmd.PersistentFlags(), "")
	require.NoError(t, err)
	assert.EqualValues(t, 29, got.DeviceID)
}

func TestGlobalFlags_ReloadUsesDefaults(t *testing.T) {
	resetRootFlags(t)
	t.Setenv("P4CTL_ADDR", "first.test:9559")
	loaded, err := loadGlobalFlags(rootCmd.PersistentFlags(), "")
	require.NoError(t, err)
	g = loaded
	t.Setenv("P4CTL_ADDR", "")
	loaded, err = loadGlobalFlags(rootCmd.PersistentFlags(), "")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9559", loaded.Addr)
}

func TestInitConfig_UnreadableFile(t *testing.T) {
	resetRootFlags(t)
	file := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(file, []byte("{}\n"), 0o600))
	require.NoError(t, os.Chmod(file, 0))
	t.Cleanup(func() { os.Chmod(file, 0o600) })
	if opened, err := os.Open(file); err == nil {
		opened.Close()
		t.Skip("file permissions do not deny reads on this platform or for this user")
	}
	require.NoError(t, rootCmd.PersistentFlags().Set("config-file", file))
	require.ErrorIs(t, initConfig(), os.ErrPermission)
}

func TestInitConfig_ExplicitFileErrors(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.yaml")
	require.NoError(t, os.WriteFile(broken, []byte("addr: [\n"), 0o600))
	for _, path := range []string{filepath.Join(dir, "missing.yaml"), broken, dir} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			resetRootFlags(t)
			require.NoError(t, rootCmd.PersistentFlags().Set("config", path))
			require.Error(t, initConfig())
		})
	}
}
