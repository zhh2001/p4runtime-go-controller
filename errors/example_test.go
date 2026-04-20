package errors_test

import (
	stderrors "errors"
	"fmt"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

func ExampleErrNotPrimary() {
	err := fmt.Errorf("write: %w", errs.ErrNotPrimary)
	fmt.Println(stderrors.Is(err, errs.ErrNotPrimary))
	// Output: true
}
