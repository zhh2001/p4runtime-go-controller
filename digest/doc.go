// Package digest subscribes to P4Runtime digest notifications by P4Info name
// or alias and acknowledges batches with IDs declared in that P4Info.
// Subscribe reports invalid names and handlers. OnDigest retains its
// cancellation-only API and registers nothing for invalid subscriptions.
// Callbacks receive the raw DigestList and decode its P4Data themselves.
package digest
