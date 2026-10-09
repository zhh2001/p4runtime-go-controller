// Package metrics reserves a namespace for future metrics support.
// It currently provides no collector interface, emitted measurements or adapters.
//
// Applications can instrument RPCs with client.WithUnaryInterceptor and
// client.WithStreamInterceptor, and observe session state with Client.State
// and Client.Events. Prometheus and OpenTelemetry adapters are planned.
package metrics
