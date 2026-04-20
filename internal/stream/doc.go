// Package stream implements the StreamChannel supervisor: a single
// goroutine that owns the bidirectional P4Runtime stream, re-sends
// arbitration on reconnect, and emits mastership events.
package stream
