// Package integration hosts the end-to-end test suite. Tests here require a
// live P4Runtime target (typically BMv2 started via scripts/run-bmv2.sh).
// They are compiled only when the `integration` build tag is set.
//
//	make e2e
//
// runs the suite with the tag wired in.
package integration
