package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
)

// dialClient uses the global flags to build a Client and waits for primary.
func dialClient(ctx context.Context) (*client.Client, error) {
	cfg, err := clientTLSConfig(g)
	if err != nil {
		return nil, err
	}
	opts := []client.Option{
		client.WithDeviceID(g.DeviceID),
		client.WithElectionID(client.ElectionID{Low: g.Election}),
	}
	if g.Role != "" {
		opts = append(opts, client.WithRole(g.Role))
	}
	if g.Insecure {
		opts = append(opts, client.WithInsecure())
	} else {
		opts = append(opts, client.WithTLS(cfg))
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

func clientTLSConfig(flags globalFlags) (*tls.Config, error) {
	if flags.Insecure {
		if flags.TLSCA != "" || flags.TLSServerName != "" || flags.TLSCert != "" || flags.TLSKey != "" {
			return nil, fmt.Errorf("TLS options require --insecure=false")
		}
		return nil, nil
	}
	if (flags.TLSCert == "") != (flags.TLSKey == "") {
		return nil, fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: flags.TLSServerName}
	if flags.TLSCA != "" {
		data, err := os.ReadFile(flags.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("read TLS CA bundle: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("TLS CA bundle contains no valid certificates")
		}
	}
	if flags.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(flags.TLSCert, flags.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("load TLS client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
