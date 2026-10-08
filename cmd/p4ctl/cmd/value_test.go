package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

func TestDecodeValue_Forms(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		width int
		hex   string
	}{
		{"0", 1, "00"}, {"0001", 9, "01"}, {"511", 9, "01ff"},
		{"18446744073709551615", 64, "ffffffffffffffff"},
		{"18446744073709551616", 65, "010000000000000000"},
		{"340282366920938463463374607431768211455", 128, "ffffffffffffffffffffffffffffffff"},
		{"0", 128, "00"}, {"1", 129, "01"},
		{"0x0", 9, "00"}, {"0x1ff", 9, "01ff"}, {"0X0001FF", 9, "01ff"},
		{"0x000000000000000000000000000000000001", 8, "01"},
		{"01:02:03:04:05:06:07:08", 128, "010002000300040005000600070008"},
		{"0x01:02:03:04:05:06:07:08", 64, "0102030405060708"},
		{"0X00:01:FF", 9, "01ff"},
		{"00:01", 9, "01"}, {"01:ff", 9, "01ff"},
		{"00:11:22:33:44:55", 48, "1122334455"},
		{"0.0.0.0", 32, "00"}, {"192.0.2.1", 32, "c0000201"},
		{"::", 128, "00"}, {"::1", 128, "01"},
		{"2001:db8::1", 128, "20010db8000000000000000000000001"},
		{"::ffff:192.0.2.1", 128, "ffffc0000201"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			want, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)
			got, err := decodeValue(tc.raw, tc.width)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func wideTableFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(tableFixture(t))
	require.NoError(t, err)
	info := &p4configv1.P4Info{}
	require.NoError(t, prototext.Unmarshal(data, info))
	info.Actions[0].Preamble.Alias = "forward"
	info.Actions[0].Params = append(info.Actions[0].Params, &p4configv1.Action_Param{Id: 2, Name: "value", Bitwidth: 129})
	for _, table := range info.Tables {
		table.MatchFields[0].Bitwidth = 128
	}
	path := filepath.Join(t.TempDir(), "wide.p4info.txtpb")
	require.NoError(t, os.WriteFile(path, []byte(prototext.Format(info)), 0o600))
	return path
}

func TestTableWrite_WideValues(t *testing.T) {
	info := wideTableFixture(t)
	for _, tc := range []struct {
		kind     string
		match    string
		priority int32
		key      string
		param    string
		value    string
	}{
		{"EXACT", "key=18446744073709551616", 0, "010000000000000000", "340282366920938463463374607431768211456", "0100000000000000000000000000000000"},
		{"EXACT", "key=2001:db8::1", 0, "20010db8000000000000000000000001", "2001:db8::2", "20010db8000000000000000000000002"},
		{"EXACT", "key=0x01:02:03:04:05:06:07:08", 0, "0102030405060708", "01:02:03:04:05:06:07:08", "010002000300040005000600070008"},
		{"LPM", "key=2001:db8:1234::abcd/64", 0, "20010db8123400000000000000000000", "192.0.2.1", "c0000201"},
		{"TERNARY", "key=2001:db8::1234&ffff:ffff:ffff:ffff::", 10, "20010db8000000000000000000000000", "0X0001FF", "01ff"},
		{"RANGE", "key=18446744073709551616..18446744073709551617", 10, "010000000000000000", "00:12:34", "1234"},
		{"OPTIONAL", "key=?340282366920938463463374607431768211455", 10, "ffffffffffffffffffffffffffffffff", "0", "00"},
	} {
		for _, update := range []client.UpdateType{client.UpdateInsert, client.UpdateModify} {
			t.Run(update.String()+"/"+tc.match, func(t *testing.T) {
				mock, addr := startDialServer(t)
				setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
				setTableFlags(t, info, "ingress.t_"+tc.kind, []string{tc.match}, tc.priority)
				tableAction, tableParams = "forward", []string{"port=0x01:ff", "value=" + tc.param}
				command := &cobra.Command{}
				command.SetContext(context.Background())
				require.NoError(t, tableWrite(command, update))
				mock.Mu.Lock()
				defer mock.Mu.Unlock()
				require.Len(t, mock.WriteRequests, 1)
				entry := mock.WriteRequests[0].Updates[0].GetEntity().GetTableEntry()
				require.Len(t, entry.Match, 1)
				match := entry.Match[0]
				var value []byte
				switch tc.kind {
				case "EXACT":
					value = match.GetExact().Value
				case "LPM":
					value = match.GetLpm().Value
					assert.EqualValues(t, 64, match.GetLpm().PrefixLen)
				case "TERNARY":
					value = match.GetTernary().Value
					assert.Equal(t, "ffffffffffffffff0000000000000000", hex.EncodeToString(match.GetTernary().Mask))
				case "RANGE":
					value = match.GetRange().Low
					assert.Equal(t, "010000000000000001", hex.EncodeToString(match.GetRange().High))
				case "OPTIONAL":
					value = match.GetOptional().Value
				}
				assert.Equal(t, tc.key, hex.EncodeToString(value))
				params := entry.GetAction().GetAction().GetParams()
				require.Len(t, params, 2)
				assert.Equal(t, []byte{1, 255}, params[0].Value)
				assert.Equal(t, tc.value, hex.EncodeToString(params[1].Value))
			})
		}
	}
}

func TestDecodeValue_Invalid(t *testing.T) {
	for _, raw := range []string{"", "x", "-1", "+1", "1.5", "1e2", " 1", "1 ", "1_000", "0b10", "0x", "0xgg", "0x-1", "0x0x01",
		"0x01::02", "0x:01", "0x01:", "0x1:02", "0x001:02", "01:2",
		"01:", ":01", "01:gg", "256.0.0.1", "192.0.2", "fe80::1%eth0", "[2001:db8::1]", "2001:db8::g"} {
		t.Run(raw, func(t *testing.T) {
			_, err := decodeValue(raw, 128)
			require.Error(t, err)
		})
	}
	for _, tc := range []struct {
		raw   string
		width int
	}{
		{"512", 9}, {"0x200", 9}, {"02:00", 9}, {"192.0.2.1", 9},
		{"2001:db8::1", 64}, {"18446744073709551616", 64},
		{"340282366920938463463374607431768211456", 128},
		{"0", 0}, {"1", -1},
		{"01:02:03:04:05:06:07:08", 64},
	} {
		t.Run(tc.raw+"/width", func(t *testing.T) {
			_, err := decodeValue(tc.raw, tc.width)
			require.Error(t, err)
		})
	}
}

func TestTableWrite_InvalidParameter(t *testing.T) {
	info := tableFixture(t)
	for _, kind := range []client.UpdateType{client.UpdateInsert, client.UpdateModify} {
		t.Run(kind.String(), func(t *testing.T) {
			for _, spec := range []string{"port=x", "port=", "port=0x", "port=-1", "port=512", "port=0x200", "port=02:00", "unknown=1", "=1", "port"} {
				t.Run(spec, func(t *testing.T) {
					mock, addr := startDialServer(t)
					setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
					setTableFlags(t, info, "ingress.t_EXACT", []string{"key=1"}, 0)
					tableAction, tableParams = "ingress.forward", []string{spec}
					command := &cobra.Command{}
					command.SetContext(context.Background())
					require.Error(t, tableWrite(command, kind))
					mock.Mu.Lock()
					defer mock.Mu.Unlock()
					assert.Empty(t, mock.WriteRequests)
					assert.Zero(t, mock.ArbitrationEchoed)
				})
			}
		})
	}
}

func TestTableWrite_InvalidWideInput(t *testing.T) {
	info := wideTableFixture(t)
	for _, tc := range []struct {
		kind  string
		match string
		param string
	}{
		{"EXACT", "key=1", "value=x"}, {"EXACT", "key=1", "value=-1"},
		{"EXACT", "key=1", "value=2001:db8::g"}, {"EXACT", "key=1", "value=0xgg"},
		{"EXACT", "key=1", "value=680564733841876926926749214863536422912"},
		{"EXACT", "key=340282366920938463463374607431768211456", "value=0"},
		{"LPM", "key=0x100000000000000000000000000000000/0", "value=0"},
		{"LPM", "key=::1/129", "value=0"}, {"LPM", "key=::1/2147483648", "value=0"},
		{"TERNARY", "key=0x100000000000000000000000000000000&0", "value=0"},
		{"TERNARY", "key=0&0x100000000000000000000000000000000", "value=0"},
		{"RANGE", "key=1..x", "value=0"}, {"RANGE", "key=2..1", "value=0"},
		{"OPTIONAL", "key=?", "value=0"},
	} {
		t.Run(tc.match+"/"+tc.param, func(t *testing.T) {
			mock, addr := startDialServer(t)
			setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, Insecure: true})
			priority := int32(0)
			if tc.kind == "TERNARY" || tc.kind == "RANGE" || tc.kind == "OPTIONAL" {
				priority = 1
			}
			setTableFlags(t, info, "ingress.t_"+tc.kind, []string{tc.match}, priority)
			tableAction, tableParams = "forward", []string{"port=1", tc.param}
			command := &cobra.Command{}
			command.SetContext(context.Background())
			require.Error(t, tableWrite(command, client.UpdateInsert))
			mock.Mu.Lock()
			defer mock.Mu.Unlock()
			assert.Empty(t, mock.WriteRequests)
			assert.Zero(t, mock.ArbitrationEchoed)
		})
	}
}

func FuzzDecodeValue(f *testing.F) {
	for _, raw := range []string{"0", "x", "-1", "0x", "0x1ff", "01:02", "2001:db8::1", "::ffff:192.0.2.1", "340282366920938463463374607431768211455"} {
		f.Add(raw, uint16(128))
	}
	f.Fuzz(func(t *testing.T, raw string, width uint16) {
		bitwidth := int(width%512) + 1
		value, err := decodeValue(raw, bitwidth)
		if err != nil {
			return
		}
		require.NotEmpty(t, value)
		if len(value) > 1 {
			require.NotZero(t, value[0], "noncanonical value for %q", raw)
		}
		number := new(big.Int).SetBytes(value)
		require.LessOrEqual(t, number.BitLen(), bitwidth)
		// Equivalent literal forms must decode to the same integer.
		decimal, err := decodeValue(number.String(), bitwidth)
		require.NoError(t, err)
		require.Equal(t, value, decimal)
		hexadecimal, err := decodeValue(fmt.Sprintf("0x%x", value), bitwidth)
		require.NoError(t, err)
		require.Equal(t, value, hexadecimal)
		if strings.HasPrefix(raw, "-") {
			t.Fatalf("accepted a negative literal: %q", raw)
		}
	})
}
