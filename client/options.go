package client

import (
	"crypto/tls"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Default knobs. Tuned to match long-running controller sessions.
const (
	defaultKeepaliveTime                 = 30 * time.Second
	defaultKeepaliveTimeout              = 10 * time.Second
	defaultReconnectBackoffInitial       = 500 * time.Millisecond
	defaultReconnectBackoffMax           = 30 * time.Second
	defaultReconnectBackoffJitterPercent = 20
	defaultArbitrationTimeout            = 10 * time.Second
	defaultMaxCallRecvSize               = 32 * 1024 * 1024 // 32 MiB
	defaultMaxCallSendSize               = 32 * 1024 * 1024
)

// options captures every tunable parameter produced by the functional Option
// helpers. It is not exported; callers interact with it only through Option.
type options struct {
	deviceID         uint64
	electionID       ElectionID
	role             string
	tlsConfig        *tls.Config
	credentials      credentials.TransportCredentials
	keepaliveTime    time.Duration
	keepaliveTimeout time.Duration
	backoffInitial   time.Duration
	backoffMax       time.Duration
	arbitrationTO    time.Duration
	maxRecvSize      int
	maxSendSize      int
	dialOpts         []grpc.DialOption
	unaryInts        []grpc.UnaryClientInterceptor
	streamInts       []grpc.StreamClientInterceptor
	logger           *slog.Logger
}

func defaultOptions() options {
	return options{
		keepaliveTime:    defaultKeepaliveTime,
		keepaliveTimeout: defaultKeepaliveTimeout,
		backoffInitial:   defaultReconnectBackoffInitial,
		backoffMax:       defaultReconnectBackoffMax,
		arbitrationTO:    defaultArbitrationTimeout,
		maxRecvSize:      defaultMaxCallRecvSize,
		maxSendSize:      defaultMaxCallSendSize,
		logger:           slog.Default(),
	}
}

// Option configures a Client at Dial time. Options are applied in the order
// supplied, so a later option overrides an earlier one that touches the same
// field.
type Option func(*options)

// WithDeviceID sets the P4Runtime device_id. Required.
func WithDeviceID(id uint64) Option {
	return func(o *options) { o.deviceID = id }
}

// WithElectionID sets the initial 128-bit election ID used by arbitration.
// Required, and must be non-zero.
func WithElectionID(id ElectionID) Option {
	return func(o *options) { o.electionID = id }
}

// WithRole sets the role name sent alongside arbitration. The default is the
// empty string, which the target interprets as the full-access role.
func WithRole(name string) Option {
	return func(o *options) { o.role = name }
}

// WithTLS enables TLS using the supplied configuration. If cfg.MinVersion is
// zero it is raised to TLS 1.2 to match the project's security baseline.
func WithTLS(cfg *tls.Config) Option {
	return func(o *options) {
		if cfg == nil {
			return
		}
		clone := cfg.Clone()
		if clone.MinVersion == 0 {
			clone.MinVersion = tls.VersionTLS12
		}
		o.tlsConfig = clone
		o.credentials = credentials.NewTLS(clone)
	}
}

// WithInsecure disables TLS. Useful for local BMv2 development. Never use in
// production against an untrusted network.
func WithInsecure() Option {
	return func(o *options) {
		o.tlsConfig = nil
		o.credentials = insecure.NewCredentials()
	}
}

// WithCredentials lets callers supply their own TransportCredentials, for
// example mTLS with a custom RootCAs pool.
func WithCredentials(c credentials.TransportCredentials) Option {
	return func(o *options) { o.credentials = c }
}

// WithKeepalive overrides the gRPC keepalive ping interval and timeout.
func WithKeepalive(pingEvery, timeout time.Duration) Option {
	return func(o *options) {
		o.keepaliveTime = pingEvery
		o.keepaliveTimeout = timeout
	}
}

// WithReconnectBackoff overrides the exponential backoff used by the stream
// supervisor. jitter is applied at ±20% of the current delay.
func WithReconnectBackoff(initial, max time.Duration) Option {
	return func(o *options) {
		o.backoffInitial = initial
		o.backoffMax = max
	}
}

// WithArbitrationTimeout bounds how long Dial will wait for the first
// MasterArbitrationUpdate response from the target before failing.
func WithArbitrationTimeout(d time.Duration) Option {
	return func(o *options) { o.arbitrationTO = d }
}

// WithMaxMessageSize overrides the gRPC max call send/receive size. Default is
// 32 MiB, which accommodates most production pipelines.
func WithMaxMessageSize(bytes int) Option {
	return func(o *options) {
		o.maxRecvSize = bytes
		o.maxSendSize = bytes
	}
}

// WithDialOptions appends raw grpc.DialOption values. It is an escape hatch
// for advanced use cases (custom resolver, load balancer, etc.).
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dialOpts = append(o.dialOpts, opts...) }
}

// WithUnaryInterceptor appends a unary gRPC interceptor. Multiple calls chain.
func WithUnaryInterceptor(ic grpc.UnaryClientInterceptor) Option {
	return func(o *options) { o.unaryInts = append(o.unaryInts, ic) }
}

// WithStreamInterceptor appends a stream gRPC interceptor.
func WithStreamInterceptor(ic grpc.StreamClientInterceptor) Option {
	return func(o *options) { o.streamInts = append(o.streamInts, ic) }
}

// WithLogger sets the slog.Logger used by the client and stream supervisor.
// The zero value falls back to slog.Default.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// buildGRPCDialOptions translates o into a grpc.DialOption slice. The result
// is deterministic for a given options value.
func (o options) buildGRPCDialOptions() []grpc.DialOption {
	creds := o.credentials
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	out := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                o.keepaliveTime,
			Timeout:             o.keepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(o.maxRecvSize),
			grpc.MaxCallSendMsgSize(o.maxSendSize),
		),
	}
	if len(o.unaryInts) > 0 {
		out = append(out, grpc.WithChainUnaryInterceptor(o.unaryInts...))
	}
	if len(o.streamInts) > 0 {
		out = append(out, grpc.WithChainStreamInterceptor(o.streamInts...))
	}
	out = append(out, o.dialOpts...)
	return out
}
