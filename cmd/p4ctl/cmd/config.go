package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func loadGlobalFlags(flags *pflag.FlagSet, home string) (globalFlags, error) {
	var result globalFlags
	if flags.Changed("config-file") && flags.Changed("config") {
		return result, fmt.Errorf("use only one of --config-file and --config for CLI settings")
	}
	explicit := true
	switch {
	case flags.Changed("config-file"):
		result.ConfigFile, _ = flags.GetString("config-file")
	case flags.Changed("config"):
		result.ConfigPath, _ = flags.GetString("config")
		result.ConfigFile = result.ConfigPath
	case os.Getenv("P4CTL_CONFIG_FILE") != "":
		result.ConfigFile = os.Getenv("P4CTL_CONFIG_FILE")
	default:
		explicit = false
		if home != "" {
			result.ConfigFile = filepath.Join(home, ".p4ctl.yaml")
		}
	}
	if explicit && result.ConfigFile == "" {
		return result, fmt.Errorf("CLI config file path must not be empty")
	}

	registry := viper.NewCodecRegistry()
	registry.RegisterCodec("json", configJSONCodec{})
	v := viper.NewWithOptions(viper.WithDecoderRegistry(registry))
	v.SetEnvPrefix("P4CTL")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	// Use declared defaults, not values left over from an earlier invocation.
	flags.VisitAll(func(flag *pflag.Flag) { v.SetDefault(flag.Name, flag.DefValue) })
	if err := v.BindPFlags(flags); err != nil {
		return result, fmt.Errorf("bind CLI settings: %w", err)
	}
	if result.ConfigFile != "" {
		v.SetConfigFile(result.ConfigFile)
		if err := v.ReadInConfig(); err != nil && (explicit || !errors.Is(err, os.ErrNotExist)) {
			return result, fmt.Errorf("read CLI config %q: %w", result.ConfigFile, err)
		}
	}

	for _, setting := range []struct {
		name  string
		value *string
	}{
		{"addr", &result.Addr}, {"role", &result.Role}, {"output", &result.Output},
		{"tls-ca", &result.TLSCA}, {"tls-server-name", &result.TLSServerName},
		{"tls-cert", &result.TLSCert}, {"tls-key", &result.TLSKey},
	} {
		value, ok := v.Get(setting.name).(string)
		if !ok {
			return result, fmt.Errorf("CLI setting %q must be a string", setting.name)
		}
		*setting.value = value
	}
	for _, setting := range []struct {
		name  string
		value *uint64
	}{
		{"device-id", &result.DeviceID}, {"election-id", &result.Election},
	} {
		raw := v.Get(setting.name)
		switch raw.(type) {
		case float32, float64:
			return result, fmt.Errorf("CLI setting %q must be an unsigned integer", setting.name)
		}
		value, err := strconv.ParseUint(fmt.Sprint(raw), 0, 64)
		if err != nil {
			return result, fmt.Errorf("CLI setting %q: %w", setting.name, err)
		}
		*setting.value = value
	}
	raw := v.Get("insecure")
	switch value := raw.(type) {
	case bool:
		result.Insecure = value
	case string:
		boolean, err := strconv.ParseBool(value)
		if err != nil {
			return result, fmt.Errorf("CLI setting %q: %w", "insecure", err)
		}
		result.Insecure = boolean
	default:
		return result, fmt.Errorf("CLI setting %q must be a boolean", "insecure")
	}
	if err := validateOutput(result.Output); err != nil {
		return result, err
	}
	return result, nil
}

// JSON numbers must retain all bits of device and election IDs.
type configJSONCodec struct{}

func (configJSONCodec) Encode(value map[string]any) ([]byte, error) { return json.Marshal(value) }

func (configJSONCodec) Decode(data []byte, value map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple JSON values in CLI config")
	}
	return nil
}
