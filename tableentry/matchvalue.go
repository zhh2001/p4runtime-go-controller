package tableentry

// MatchValue is a marker interface implemented by every kind of match
// constructor produced by this package. Implementations are unexported by
// design — callers obtain values through Exact, LPM, Ternary, Range, and
// Optional helpers rather than constructing structs directly.
type MatchValue interface {
	isMatchValue()
}

// ExactMatch carries a single value that must compare byte-for-byte equal.
type ExactMatch struct{ Value []byte }

func (ExactMatch) isMatchValue() {}

// LPMMatch carries a value plus a prefix length in bits.
type LPMMatch struct {
	Value     []byte
	PrefixLen int32
}

func (LPMMatch) isMatchValue() {}

// TernaryMatch carries a value and a mask of equal length. The caller is
// responsible for ensuring value & mask == value (otherwise the target
// likely rejects the entry).
type TernaryMatch struct{ Value, Mask []byte }

func (TernaryMatch) isMatchValue() {}

// RangeMatch carries an inclusive [low, high] range. low and high share the
// same bit width as the match field.
type RangeMatch struct{ Low, High []byte }

func (RangeMatch) isMatchValue() {}

// OptionalMatch carries an optional value. A nil value means "don't care"
// and the match field is omitted from the wire entry.
type OptionalMatch struct{ Value []byte }

func (OptionalMatch) isMatchValue() {}

// Exact returns an EXACT match constructor.
func Exact(v []byte) MatchValue { return ExactMatch{Value: v} }

// LPM returns an LPM match constructor. prefixLen is counted in bits.
func LPM(v []byte, prefixLen int32) MatchValue {
	return LPMMatch{Value: v, PrefixLen: prefixLen}
}

// Ternary returns a TERNARY match constructor.
func Ternary(v, mask []byte) MatchValue { return TernaryMatch{Value: v, Mask: mask} }

// Range returns a RANGE match constructor with an inclusive [low, high].
func Range(low, high []byte) MatchValue { return RangeMatch{Low: low, High: high} }

// Optional returns an OPTIONAL match constructor. A nil value is treated as
// don't-care and the field is omitted from the wire entry.
func Optional(v []byte) MatchValue { return OptionalMatch{Value: v} }
