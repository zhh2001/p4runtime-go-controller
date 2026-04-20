package client

import (
	"fmt"
	"math/big"
)

// ElectionID is the 128-bit mastership election value used by P4Runtime.
//
// The pair (High, Low) is interpreted as an unsigned 128-bit integer in
// big-endian order, where High holds the most significant 64 bits. The zero
// value (High=0, Low=0) is reserved by the P4Runtime specification to mean
// "no participation in the election"; comparisons treat it as less than any
// non-zero value.
type ElectionID struct {
	High uint64
	Low  uint64
}

// IsZero reports whether e is the reserved all-zero election ID.
func (e ElectionID) IsZero() bool {
	return e.High == 0 && e.Low == 0
}

// Equal reports whether e and other represent the same 128-bit value.
func (e ElectionID) Equal(other ElectionID) bool {
	return e.High == other.High && e.Low == other.Low
}

// Less reports whether e is strictly less than other when both are
// interpreted as unsigned 128-bit integers.
func (e ElectionID) Less(other ElectionID) bool {
	if e.High != other.High {
		return e.High < other.High
	}
	return e.Low < other.Low
}

// Cmp returns -1, 0, or 1 depending on whether e is less than, equal to, or
// greater than other (unsigned 128-bit comparison).
func (e ElectionID) Cmp(other ElectionID) int {
	switch {
	case e.Less(other):
		return -1
	case e.Equal(other):
		return 0
	default:
		return 1
	}
}

// Increment returns (e+1, true) on success. When e is the maximum value
// (all ones in both halves) it returns (e, false) rather than wrapping —
// silently rolling over would invite collisions that the caller cannot
// detect.
func (e ElectionID) Increment() (ElectionID, bool) {
	if e.Low == ^uint64(0) {
		if e.High == ^uint64(0) {
			return e, false
		}
		return ElectionID{High: e.High + 1, Low: 0}, true
	}
	return ElectionID{High: e.High, Low: e.Low + 1}, true
}

// String formats e as "high:low" in decimal, matching the representation
// typically printed in P4Runtime server logs.
func (e ElectionID) String() string {
	return fmt.Sprintf("%d:%d", e.High, e.Low)
}

// BigInt returns e as a math/big unsigned integer. It allocates; prefer the
// Cmp / Less / Equal helpers in hot paths.
func (e ElectionID) BigInt() *big.Int {
	v := new(big.Int).SetUint64(e.High)
	v.Lsh(v, 64)
	return v.Or(v, new(big.Int).SetUint64(e.Low))
}
