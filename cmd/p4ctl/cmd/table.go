package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

var (
	tableName     string
	tableMatches  []string
	tableAction   string
	tableParams   []string
	tableP4Info   string
	tablePriority int32
)

var tableCmd = &cobra.Command{
	Use:   "table",
	Short: "Insert, modify, delete, or read table entries",
}

var tableInsertCmd = &cobra.Command{
	Use:   "insert",
	Short: "Insert an entry into the specified table",
	RunE:  func(cmd *cobra.Command, _ []string) error { return tableWrite(cmd, client.UpdateInsert) },
}
var tableModifyCmd = &cobra.Command{
	Use:   "modify",
	Short: "Modify an entry in the specified table",
	RunE:  func(cmd *cobra.Command, _ []string) error { return tableWrite(cmd, client.UpdateModify) },
}
var tableDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an entry from the specified table",
	RunE:  func(cmd *cobra.Command, _ []string) error { return tableWrite(cmd, client.UpdateDelete) },
}

var tableReadCmd = &cobra.Command{
	Use:   "read",
	Short: "Read all entries of the specified table",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if tableName == "" || tableP4Info == "" {
			return fmt.Errorf("--table and --p4info required")
		}
		p, err := readPipeline(tableP4Info)
		if err != nil {
			return err
		}
		td, ok := p.Table(tableName)
		if !ok {
			return fmt.Errorf("table %q not in pipeline", tableName)
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		c, err := dialClient(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		entries, err := c.ReadTableEntries(ctx, td.ID)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Fprintf(cmd.OutOrStdout(), "%s id=%d priority=%d match=%d action=%s\n",
				td.Name, e.GetTableId(), e.GetPriority(), len(e.GetMatch()),
				e.GetAction().GetAction())
		}
		return nil
	},
}

func tableWrite(cmd *cobra.Command, kind client.UpdateType) error {
	if tableName == "" || tableP4Info == "" {
		return fmt.Errorf("--table and --p4info required")
	}
	if kind != client.UpdateDelete && tableAction == "" {
		return fmt.Errorf("--action required for %s", kind)
	}
	p, err := readPipeline(tableP4Info)
	if err != nil {
		return err
	}
	b := tableentry.NewBuilder(p, tableName)
	for _, m := range tableMatches {
		if err := applyMatch(b, p, tableName, m); err != nil {
			return err
		}
	}
	if kind != client.UpdateDelete {
		params := make([]tableentry.ActionParam, 0, len(tableParams))
		for _, prm := range tableParams {
			ap, err := parseActionParam(prm)
			if err != nil {
				return err
			}
			params = append(params, ap)
		}
		b.Action(tableAction, params...)
		if tablePriority != 0 {
			b.Priority(tablePriority)
		}
	}
	entry, err := b.Build()
	if err != nil {
		// Build requires an action on every path; for DELETE we still
		// need one logical path so let builder enforce its invariants.
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	c, err := dialClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.WriteTableEntry(ctx, kind, entry)
}

// applyMatch parses a single "field=value" string. Rules:
//
//   - "name=value" → EXACT
//   - "name=value/prefix" → LPM
//   - "name=value&mask" → TERNARY
//   - "name=low..high" → RANGE
//   - "name=?value" → OPTIONAL (optional present)
//
// Values are interpreted based on common heuristics: colon-separated bytes
// look like MAC/hex, dotted quads look like IPv4, plain numbers become the
// canonical bytes for the declared bit width.
func applyMatch(b *tableentry.Builder, p *pipeline.Pipeline, table, spec string) error {
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return fmt.Errorf("bad match %q: expected field=value", spec)
	}
	name := spec[:eq]
	raw := spec[eq+1:]
	td, _ := p.Table(table)
	if td == nil {
		return fmt.Errorf("table %q not in pipeline", table)
	}
	mf, ok := td.MatchField(name)
	if !ok {
		return fmt.Errorf("field %q not on table %q", name, table)
	}

	switch {
	case strings.Contains(raw, "/"):
		parts := strings.SplitN(raw, "/", 2)
		v, err := decodeValue(parts[0], int(mf.Bitwidth))
		if err != nil {
			return err
		}
		prefix, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("prefix %q: %w", parts[1], err)
		}
		if prefix < 0 || prefix > math.MaxInt32 {
			return fmt.Errorf("prefix %q out of int32 range", parts[1])
		}
		b.Match(name, tableentry.LPM(v, int32(prefix)))
	case strings.Contains(raw, "&"):
		parts := strings.SplitN(raw, "&", 2)
		v, err := decodeValue(parts[0], int(mf.Bitwidth))
		if err != nil {
			return err
		}
		m, err := decodeValue(parts[1], int(mf.Bitwidth))
		if err != nil {
			return err
		}
		b.Match(name, tableentry.Ternary(v, m))
	case strings.Contains(raw, ".."):
		parts := strings.SplitN(raw, "..", 2)
		low, err := decodeValue(parts[0], int(mf.Bitwidth))
		if err != nil {
			return err
		}
		high, err := decodeValue(parts[1], int(mf.Bitwidth))
		if err != nil {
			return err
		}
		b.Match(name, tableentry.Range(low, high))
	case strings.HasPrefix(raw, "?"):
		v, err := decodeValue(strings.TrimPrefix(raw, "?"), int(mf.Bitwidth))
		if err != nil {
			return err
		}
		b.Match(name, tableentry.Optional(v))
	default:
		v, err := decodeValue(raw, int(mf.Bitwidth))
		if err != nil {
			return err
		}
		b.Match(name, tableentry.Exact(v))
	}
	return nil
}

func decodeValue(raw string, bitwidth int) ([]byte, error) {
	switch {
	case strings.Contains(raw, ":"):
		return codec.MAC(raw)
	case strings.Count(raw, ".") == 3:
		return codec.IPv4(raw)
	case strings.HasPrefix(raw, "0x"):
		return codec.ParseHex(raw)
	default:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value %q: %w", raw, err)
		}
		return codec.EncodeUint(n, bitwidth)
	}
}

func parseActionParam(spec string) (tableentry.ActionParam, error) {
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return tableentry.ActionParam{}, fmt.Errorf("bad param %q: expected name=value", spec)
	}
	return tableentry.ActionParam{
		Name:  spec[:eq],
		Value: unsafeDecode(spec[eq+1:]),
	}, nil
}

// unsafeDecode picks a reasonable byte representation for action parameters,
// favoring numeric / hex / dotted-quad / colon-hex forms.
func unsafeDecode(raw string) []byte {
	switch {
	case strings.Contains(raw, ":"):
		if b, err := codec.MAC(raw); err == nil {
			return b
		}
		if b, err := codec.ParseHex(raw); err == nil {
			return b
		}
	case strings.Count(raw, ".") == 3:
		if b, err := codec.IPv4(raw); err == nil {
			return b
		}
	case strings.HasPrefix(raw, "0x"):
		if b, err := codec.ParseHex(raw); err == nil {
			return b
		}
	}
	if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
		// Default to 64-bit canonical encoding; the builder / pipeline
		// will reject over-wide values.
		if b, err := codec.EncodeUint(n, 64); err == nil {
			return b
		}
	}
	return []byte(raw)
}

func readPipeline(path string) (*pipeline.Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read p4info: %w", err)
	}
	return pipeline.LoadText(raw, nil)
}

func init() {
	for _, sub := range []*cobra.Command{tableInsertCmd, tableModifyCmd, tableDeleteCmd, tableReadCmd} {
		sub.Flags().StringVar(&tableName, "table", "", "fully qualified table name (required)")
		sub.Flags().StringVar(&tableP4Info, "p4info", "", "path to P4Info text proto (required)")
	}
	for _, sub := range []*cobra.Command{tableInsertCmd, tableModifyCmd, tableDeleteCmd} {
		sub.Flags().StringSliceVar(&tableMatches, "match", nil, "match spec(s): field=value, field=value/prefix, field=value&mask, field=low..high, field=?value")
	}
	for _, sub := range []*cobra.Command{tableInsertCmd, tableModifyCmd} {
		sub.Flags().StringVar(&tableAction, "action", "", "action name")
		sub.Flags().StringSliceVar(&tableParams, "param", nil, "action param(s): name=value")
		sub.Flags().Int32Var(&tablePriority, "priority", 0, "priority (required for ternary/range/optional)")
	}
	tableCmd.AddCommand(tableInsertCmd, tableModifyCmd, tableDeleteCmd, tableReadCmd)
}
