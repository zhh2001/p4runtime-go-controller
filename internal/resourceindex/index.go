// Package resourceindex validates indexes for indirect P4Runtime resources.
package resourceindex

import "fmt"

// Validate checks a resource index. Reads can allow -1 to select every entry.
// Named index types may require target-side translation, so their upper bound
// cannot be inferred from the array size.
func Validate(index, size int64, indexType string, allowAll bool) error {
	if allowAll && index == -1 {
		return nil
	}
	if index < 0 {
		if allowAll {
			return fmt.Errorf("index must be -1 or non-negative, got %d", index)
		}
		return fmt.Errorf("index must be non-negative, got %d", index)
	}
	if indexType == "" {
		if size <= 0 {
			return fmt.Errorf("invalid array size %d for index %d", size, index)
		}
		if index >= size {
			return fmt.Errorf("index %d outside array range [0, %d)", index, size)
		}
	}
	return nil
}
