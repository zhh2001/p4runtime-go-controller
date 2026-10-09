// Package pipeline models the P4 forwarding pipeline: parsed P4Info and the
// opaque target-specific device configuration blob. It also exposes the
// by-name index used by the builders in the tableentry, counter, meter,
// register, and digest packages.
// Pipelines own their inputs and return independent messages and definitions.
package pipeline
