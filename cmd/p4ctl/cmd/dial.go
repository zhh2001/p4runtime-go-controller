package cmd

import (
	"context"
	"fmt"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

// dialClient uses the global flags to build a Client. It always uses insecure
// transport when g.Insecure is true; TLS is out of scope for this early CLI
// (users can always embed the library directly if they need mTLS right now).
func dialClient(ctx context.Context) (*client.Client, error) {
	opts := []client.Option{
		client.WithDeviceID(g.DeviceID),
		client.WithElectionID(client.ElectionID{Low: g.Election}),
	}
	if g.Role != "" {
		opts = append(opts, client.WithRole(g.Role))
	}
	if g.Insecure {
		opts = append(opts, client.WithInsecure())
	}
	c, err := client.Dial(ctx, g.Addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", g.Addr, err)
	}
	if err := c.BecomePrimary(ctx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("arbitration: %w", err)
	}
	return c, nil
}
