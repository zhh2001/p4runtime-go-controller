package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type tlsTestIdentity struct {
	certFile string
	keyFile  string
	cert     tls.Certificate
}

type tlsTestCA struct {
	file string
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTLSCA(t *testing.T) tlsTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	file := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(file, data, 0o600))
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(data))
	return tlsTestCA{file: file, cert: cert, key: key, pool: pool}
}

func (ca tlsTestCA) identity(t *testing.T, client, expired bool) tlsTestIdentity {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "switch.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"switch.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		template.Subject.CommonName = "controller.test"
	}
	if expired {
		template.NotBefore = time.Now().Add(-2 * time.Hour)
		template.NotAfter = time.Now().Add(-time.Hour)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	return tlsTestIdentity{certFile: certFile, keyFile: keyFile, cert: cert}
}

func TestDialClient_Transport(t *testing.T) {
	ca, otherCA := newTLSCA(t), newTLSCA(t)
	server, expiredServer := ca.identity(t, false, false), ca.identity(t, false, true)
	controller, otherController := ca.identity(t, true, false), otherCA.identity(t, true, false)
	for _, tc := range []struct {
		name       string
		server     *tls.Config
		flags      globalFlags
		wantOK     bool
		wantClient bool
	}{
		{name: "plaintext", flags: globalFlags{Insecure: true}, wantOK: true},
		{name: "TLS 1.2", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}, flags: globalFlags{TLSCA: ca.file}, wantOK: true},
		{name: "TLS 1.3 with server name", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS13}, flags: globalFlags{TLSCA: ca.file, TLSServerName: "switch.test"}, wantOK: true},
		{name: "mutual TLS", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.pool}, flags: globalFlags{TLSCA: ca.file, TLSCert: controller.certFile, TLSKey: controller.keyFile}, wantOK: true, wantClient: true},
		{name: "untrusted server", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12}, flags: globalFlags{TLSCA: otherCA.file}},
		{name: "private root absent from defaults", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12}},
		{name: "wrong server name", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12}, flags: globalFlags{TLSCA: ca.file, TLSServerName: "wrong.test"}},
		{name: "expired server", server: &tls.Config{Certificates: []tls.Certificate{expiredServer.cert}, MinVersion: tls.VersionTLS12}, flags: globalFlags{TLSCA: ca.file}},
		{name: "client certificate required", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.pool}, flags: globalFlags{TLSCA: ca.file}},
		{name: "untrusted client", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.pool}, flags: globalFlags{TLSCA: ca.file, TLSCert: otherController.certFile, TLSKey: otherController.keyFile}},
		{name: "TLS 1.1", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}, flags: globalFlags{TLSCA: ca.file}},
		{name: "plaintext client to TLS server", server: &tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12}, flags: globalFlags{Insecure: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := make(chan credentials.TLSInfo, 1)
			var opts []grpc.ServerOption
			if tc.server != nil {
				opts = append(opts, grpc.Creds(credentials.NewTLS(tc.server)))
			}
			opts = append(opts, grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				p, ok := peer.FromContext(stream.Context())
				if ok {
					if info, ok := p.AuthInfo.(credentials.TLSInfo); ok {
						auth <- info
					}
				}
				return handler(srv, stream)
			}))
			mock, addr := startDialServer(t, opts...)
			flags := tc.flags
			flags.Addr, flags.DeviceID, flags.Election, flags.Role = addr, 1, 1, "test-role"
			setDialFlags(t, flags)
			timeout := 300 * time.Millisecond
			if tc.wantOK {
				timeout = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			c, err := dialClient(ctx)
			if c != nil {
				defer c.Close()
			}
			if !tc.wantOK {
				require.Error(t, err)
				require.Nil(t, c)
				mock.Mu.Lock()
				defer mock.Mu.Unlock()
				assert.Zero(t, mock.ArbitrationEchoed)
				return
			}
			require.NoError(t, err)
			require.True(t, c.IsPrimary())
			if tc.server != nil {
				var info credentials.TLSInfo
				select {
				case info = <-auth:
				case <-ctx.Done():
					t.Fatal("server did not observe TLS authentication")
				}
				assert.GreaterOrEqual(t, info.State.Version, uint16(tls.VersionTLS12))
				if tc.wantClient {
					require.NotEmpty(t, info.State.VerifiedChains)
					assert.Equal(t, "controller.test", info.State.PeerCertificates[0].Subject.CommonName)
				}
			}
			mock.Mu.Lock()
			defer mock.Mu.Unlock()
			assert.EqualValues(t, 1, mock.LastArbitrationUpdate.GetDeviceId())
			assert.EqualValues(t, 1, mock.LastArbitrationUpdate.GetElectionId().GetLow())
			assert.Equal(t, flags.Role, mock.LastArbitrationUpdate.GetRole().GetName())
		})
	}
}

func TestClientTLSConfig_DefaultsAndInvalidFiles(t *testing.T) {
	cfg, err := clientTLSConfig(globalFlags{})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Nil(t, cfg.RootCAs, "nil RootCAs selects the system trust store")
	assert.False(t, cfg.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	ca := newTLSCA(t)
	identity, other := ca.identity(t, true, false), ca.identity(t, true, false)
	invalid := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(invalid, []byte("not a certificate"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing.pem")
	for _, tc := range []struct {
		name  string
		flags globalFlags
		want  string
	}{
		{"CA in plaintext mode", globalFlags{Insecure: true, TLSCA: ca.file}, "--insecure=false"},
		{"name in plaintext mode", globalFlags{Insecure: true, TLSServerName: "switch.test"}, "--insecure=false"},
		{"certificate in plaintext mode", globalFlags{Insecure: true, TLSCert: identity.certFile}, "--insecure=false"},
		{"key in plaintext mode", globalFlags{Insecure: true, TLSKey: identity.keyFile}, "--insecure=false"},
		{"certificate without key", globalFlags{TLSCert: identity.certFile}, "provided together"},
		{"key without certificate", globalFlags{TLSKey: identity.keyFile}, "provided together"},
		{"missing CA", globalFlags{TLSCA: missing}, "read TLS CA bundle"},
		{"invalid CA", globalFlags{TLSCA: invalid}, "no valid certificates"},
		{"missing certificate", globalFlags{TLSCert: missing, TLSKey: identity.keyFile}, "load TLS client certificate"},
		{"missing key", globalFlags{TLSCert: identity.certFile, TLSKey: missing}, "load TLS client certificate"},
		{"invalid certificate", globalFlags{TLSCert: invalid, TLSKey: identity.keyFile}, "load TLS client certificate"},
		{"invalid key", globalFlags{TLSCert: identity.certFile, TLSKey: invalid}, "load TLS client certificate"},
		{"mismatched key", globalFlags{TLSCert: identity.certFile, TLSKey: other.keyFile}, "load TLS client certificate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := clientTLSConfig(tc.flags)
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, cfg)
			setDialFlags(t, tc.flags)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, dialErr := dialClient(ctx)
			assert.Nil(t, c)
			require.ErrorContains(t, dialErr, tc.want)
		})
	}
}

func TestDialClient_TLSBackupClosesSession(t *testing.T) {
	ca := newTLSCA(t)
	server := ca.identity(t, false, false)
	streamDone := make(chan struct{})
	mock, addr := startDialServer(t,
		grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{server.cert}, MinVersion: tls.VersionTLS12})),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			defer close(streamDone)
			return handler(srv, stream)
		}),
	)
	mock.Mu.Lock()
	mock.PrimaryElectionLow = 99
	mock.Mu.Unlock()
	setDialFlags(t, globalFlags{Addr: addr, DeviceID: 1, Election: 1, TLSCA: ca.file})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c, err := dialClient(ctx)
	require.Nil(t, c)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "arbitration")
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("backup TLS session was not closed after the primary wait")
	}
}
