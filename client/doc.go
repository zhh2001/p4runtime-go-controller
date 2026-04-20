// Package client implements the long-lived P4Runtime controller session.
//
// A Client owns the gRPC connection to a single target, the mastership state
// tracked through the bidirectional StreamChannel, and the unary call surface
// for pipeline configuration, writes, reads, and capability queries. A Client
// is safe for concurrent use by multiple goroutines.
//
// Connect with Dial:
//
//	c, err := client.Dial(ctx, "switch:9559",
//	    client.WithDeviceID(1),
//	    client.WithElectionID(client.ElectionID{High: 0, Low: 1}),
//	)
//	if err != nil {
//	    return err
//	}
//	defer c.Close()
//
// See the examples directory for end-to-end walkthroughs.
package client
