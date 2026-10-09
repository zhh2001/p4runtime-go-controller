package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

type logBuffer struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	onWrite func()
}

func (b *logBuffer) Write(p []byte) (int, error) {
	if b.onWrite != nil {
		b.onWrite()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	data := bytes.Clone(b.buffer.Bytes())
	b.mu.Unlock()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var records []map[string]any
	for decoder.More() {
		var record map[string]any
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func logRecords(records []map[string]any, message string) []map[string]any {
	var found []map[string]any
	for _, record := range records {
		if record["msg"] == message {
			found = append(found, record)
		}
	}
	return found
}

func blockingPrimary(ctx context.Context) *lifecycleStream {
	first := true
	return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
		if first {
			first = false
			return arbitrationResponse(codes.OK), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
}

func TestSupervisorConnectionLogging(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output logBuffer
		logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})).
			With("controller", "edge").WithGroup("session")
		messages := make(chan *p4v1.StreamMessageResponse, 1)
		messages <- arbitrationResponse(codes.OK)
		s := New(Config{DeviceID: 17, ElectionHigh: 2, ElectionLow: 3, Role: "routing", Logger: logger},
			func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
				return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
					select {
					case msg := <-messages:
						return msg, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}}, nil
			}, nil)
		// A writer can inspect state without acquiring a lock held by logging.
		output.onWrite = func() { _ = s.State(); _ = s.IsPrimary() }
		s.Start(context.Background())
		defer s.Close()
		synctest.Wait()
		require.True(t, s.IsPrimary())
		messages <- arbitrationResponse(codes.AlreadyExists)
		synctest.Wait()
		require.Equal(t, StateBackup, s.State())
		messages <- arbitrationResponse(codes.OK)
		synctest.Wait()
		messages <- arbitrationResponse(codes.OK)
		synctest.Wait()
		before := len(output.records(t))
		messages <- packetResponse()
		synctest.Wait()
		require.NoError(t, s.Send(context.Background(), &p4v1.StreamMessageRequest{
			Update: &p4v1.StreamMessageRequest_Packet{Packet: &p4v1.PacketOut{Payload: []byte("private packet payload")}},
		}))
		assert.Len(t, output.records(t), before, "packet delivery should not produce lifecycle logs")
		s.Close()

		records := output.records(t)
		require.Len(t, logRecords(records, "p4runtime: opening StreamChannel"), 1)
		arbitrations := logRecords(records, "p4runtime: arbitration status")
		require.Len(t, arbitrations, 4)
		for i, state := range []string{"primary", "backup", "primary", "primary"} {
			attrs := arbitrations[i]["session"].(map[string]any)
			assert.Equal(t, state, attrs["state"])
			wantCode := "OK"
			if state == "backup" {
				wantCode = "AlreadyExists"
			}
			assert.Equal(t, wantCode, attrs["status_code"])
			level := "INFO"
			if i == 3 {
				level = "DEBUG"
			}
			assert.Equal(t, level, arbitrations[i]["level"])
		}
		for _, record := range records {
			assert.Equal(t, "edge", record["controller"])
			attrs := record["session"].(map[string]any)
			assert.Equal(t, json.Number("17"), attrs["device_id"])
			assert.Equal(t, json.Number("2"), attrs["election_id_high"])
			assert.Equal(t, json.Number("3"), attrs["election_id_low"])
			assert.Equal(t, "routing", attrs["role"])
			assert.NotEqual(t, "WARN", record["level"])
		}
		require.Len(t, logRecords(records, "p4runtime: stream supervisor stopped"), 1)
	})
}

func TestSupervisorFailureLogging(t *testing.T) {
	for _, stage := range []string{"open", "arbitration_send", "arbitration_recv", "arbitration_timeout", "arbitration_message", "recv", "send"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output logBuffer
				logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
				failed := errors.New("target unavailable")
				failRecv := make(chan struct{})
				dials := 0
				s := New(Config{Logger: logger, BackoffInitial: time.Second, ArbitrationTimeout: 10 * time.Millisecond},
					func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
						dials++
						if dials > 1 {
							return blockingPrimary(ctx), nil
						}
						if stage == "open" {
							return nil, failed
						}
						first := true
						return &lifecycleStream{ctx: ctx,
							send: func(req *p4v1.StreamMessageRequest) error {
								if (stage == "arbitration_send" && req.GetArbitration() != nil) || (stage == "send" && req.GetArbitration() == nil) {
									return failed
								}
								return nil
							},
							recv: func() (*p4v1.StreamMessageResponse, error) {
								if first {
									first = false
									switch stage {
									case "arbitration_recv":
										return nil, failed
									case "arbitration_timeout":
										<-ctx.Done()
										return nil, ctx.Err()
									case "arbitration_message":
										return packetResponse(), nil
									}
									return arbitrationResponse(codes.OK), nil
								}
								select {
								case <-failRecv:
									return nil, failed
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							},
						}, nil
					}, nil)
				s.Start(context.Background())
				defer s.Close()
				synctest.Wait()
				switch stage {
				case "recv":
					close(failRecv)
				case "send":
					require.ErrorIs(t, s.Send(context.Background(), &p4v1.StreamMessageRequest{}), failed)
				}
				time.Sleep(2 * time.Second)
				synctest.Wait()
				require.True(t, s.IsPrimary())
				require.Equal(t, 2, dials)
				s.Close()
				records := output.records(t)
				failures := logRecords(records, "p4runtime: StreamChannel failed")
				require.Len(t, failures, 1)
				assert.Equal(t, "WARN", failures[0]["level"])
				wantStage := stage
				if stage != "open" && stage != "recv" && stage != "send" {
					wantStage = "arbitration"
				}
				assert.Equal(t, wantStage, failures[0]["stage"])
				wantError := failed.Error()
				if stage == "arbitration_timeout" {
					wantError = context.DeadlineExceeded.Error()
				} else if stage == "arbitration_message" {
					wantError = "expected arbitration"
				}
				assert.Contains(t, failures[0]["error"], wantError)
				assert.Len(t, logRecords(records, "p4runtime: opening StreamChannel"), 2)
				wantRetries := 1
				if stage == "recv" || stage == "send" {
					wantRetries = 0 // An established stream reconnects without a sleep.
				}
				assert.Len(t, logRecords(records, "p4runtime: StreamChannel retry scheduled"), wantRetries)
			})
		})
	}
}

func TestSupervisorRetryLogging(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output logBuffer
		s := New(Config{Logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
			BackoffInitial: time.Second, BackoffMax: 4 * time.Second},
			func(context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
				return nil, errors.New("stream not ready")
			}, nil)
		s.Start(context.Background())
		defer s.Close()
		for i, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second} {
			synctest.Wait()
			retries := logRecords(output.records(t), "p4runtime: StreamChannel retry scheduled")
			require.Len(t, retries, i+1)
			require.Equal(t, "DEBUG", retries[i]["level"])
			nanos, err := retries[i]["delay"].(json.Number).Int64()
			require.NoError(t, err)
			delay := time.Duration(nanos)
			assert.GreaterOrEqual(t, delay, base-base/5)
			assert.Less(t, delay, base+base/5)
			if i < 3 {
				time.Sleep(delay)
			}
		}
		s.Close()
		assert.Len(t, logRecords(output.records(t), "p4runtime: StreamChannel failed"), 4)
	})
}

func TestSupervisorShutdownLogging(t *testing.T) {
	for _, pending := range []string{"open", "arbitration"} {
		for _, shutdown := range []string{"close", "cancel"} {
			t.Run(pending+"/"+shutdown, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var output logBuffer
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					s := New(Config{Logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))},
						func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
							if pending == "open" {
								<-ctx.Done()
								return nil, ctx.Err()
							}
							return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
								<-ctx.Done()
								return nil, ctx.Err()
							}}, nil
						}, nil)
					s.Start(ctx)
					defer s.Close()
					synctest.Wait()
					if shutdown == "cancel" {
						cancel()
					}
					s.Close()
					records := output.records(t)
					assert.Empty(t, logRecords(records, "p4runtime: StreamChannel failed"))
					assert.Empty(t, logRecords(records, "p4runtime: StreamChannel retry scheduled"))
					assert.Len(t, logRecords(records, "p4runtime: stream supervisor stopped"), 1)
				})
			})
		}
	}
}

func TestSupervisorLoggingLevels(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		t.Run(level.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output logBuffer
				dials := 0
				s := New(Config{Logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: level}))},
					func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
						dials++
						if dials == 1 {
							return nil, errors.New("target unavailable")
						}
						return blockingPrimary(ctx), nil
					}, nil)
				s.Start(context.Background())
				defer s.Close()
				time.Sleep(time.Second)
				synctest.Wait()
				require.True(t, s.IsPrimary())
				s.Close()
				records := output.records(t)
				for _, record := range records {
					assert.NotEqual(t, "DEBUG", record["level"])
				}
				switch level {
				case slog.LevelInfo:
					assert.Len(t, records, 4) // Two attempts, a failure and successful arbitration.
				case slog.LevelWarn:
					require.Len(t, records, 1)
					assert.Equal(t, "WARN", records[0]["level"])
				case slog.LevelError:
					assert.Empty(t, records)
				}
			})
		})
	}
}

func TestSupervisorGracefulCloseLogging(t *testing.T) {
	for _, failure := range []string{"none", "close_send", "final_status"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output logBuffer
				failed := errors.New("graceful close failed")
				end := make(chan struct{})
				s := New(Config{Logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))},
					func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
						first := true
						return &lifecycleStream{ctx: ctx,
							closeSend: func() error {
								if failure == "close_send" {
									return failed
								}
								close(end)
								return nil
							},
							recv: func() (*p4v1.StreamMessageResponse, error) {
								if first {
									first = false
									return arbitrationResponse(codes.OK), nil
								}
								select {
								case <-end:
									if failure == "final_status" {
										return nil, failed
									}
									return nil, io.EOF
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							},
						}, nil
					}, nil)
				s.Start(context.Background())
				defer s.Close()
				synctest.Wait()
				err := s.CloseGracefully(context.Background())
				if failure == "none" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, failed)
				}
				s.Close()
				failures := logRecords(output.records(t), "p4runtime: StreamChannel failed")
				if failure == "none" {
					assert.Empty(t, failures)
				} else {
					require.Len(t, failures, 1)
					assert.Equal(t, "close", failures[0]["stage"])
					assert.Equal(t, failed.Error(), failures[0]["error"])
				}
				assert.Len(t, logRecords(output.records(t), "p4runtime: opening StreamChannel"), 1)
			})
		})
	}
}
