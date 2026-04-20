// Package testutil hosts the in-process P4Runtime server used by unit tests.
// It is built on google.golang.org/grpc/test/bufconn and requires no network
// access. The exported API is intentionally minimal — test files reach into
// it directly.
package testutil
