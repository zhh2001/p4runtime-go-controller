// Package metrics defines the pluggable observability surface for the SDK.
// Core packages emit counters and histograms through the Metrics interface.
// A no-op implementation is provided for users that do not opt in.
//
// Prometheus and OpenTelemetry adapters live in dedicated sub-packages so
// the core library retains zero hard dependency on them.
package metrics
