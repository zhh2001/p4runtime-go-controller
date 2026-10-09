package unit_test

import (
	"context"
	"math"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/counter"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/meter"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/register"
)

type indexedResource struct {
	read  func(context.Context, string, int64) error
	write func(context.Context, string, int64) error
	entry func(*p4v1.Entity) (uint32, *p4v1.Index)
	id    uint32
}

func newIndexedResource(t *testing.T, kind string, c *client.Client, size int64, indexType string) indexedResource {
	t.Helper()
	var typeName *p4configv1.P4NamedType
	if indexType != "" {
		typeName = &p4configv1.P4NamedType{Name: indexType}
	}
	preamble := &p4configv1.Preamble{Name: kind, Alias: kind + "_alias"}
	info := &p4configv1.P4Info{}
	switch kind {
	case "counter":
		preamble.Id = 0x12000001
		info.Counters = []*p4configv1.Counter{{
			Preamble: preamble, Size: size, IndexTypeName: typeName,
			Spec: &p4configv1.CounterSpec{Unit: p4configv1.CounterSpec_BOTH},
		}}
	case "meter":
		preamble.Id = 0x14000001
		info.Meters = []*p4configv1.Meter{{
			Preamble: preamble, Size: size, IndexTypeName: typeName,
			Spec: &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_BYTES},
		}}
	case "register":
		preamble.Id = 0x16000001
		info.Registers = []*p4configv1.Register{{
			Preamble: preamble, Size: int32(size), IndexTypeName: typeName,
			TypeSpec: &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{
				Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Bit{
					Bit: &p4configv1.P4BitTypeSpec{Bitwidth: 9},
				}},
			}},
		}}
	default:
		t.Fatalf("unknown resource kind %q", kind)
	}
	if indexType != "" {
		info.TypeInfo = &p4configv1.P4TypeInfo{NewTypes: map[string]*p4configv1.P4NewTypeSpec{
			indexType: {Representation: &p4configv1.P4NewTypeSpec_TranslatedType{
				TranslatedType: &p4configv1.P4NewTypeTranslation{Uri: "test/PortId_t", SdnType: &p4configv1.P4NewTypeTranslation_SdnBitwidth{SdnBitwidth: 64}},
			}},
		}}
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	switch kind {
	case "counter":
		r, err := counter.NewReader(c, p)
		require.NoError(t, err)
		return indexedResource{
			read: func(ctx context.Context, name string, index int64) error {
				_, err := r.Read(ctx, name, index)
				return err
			},
			write: func(ctx context.Context, name string, index int64) error { return r.Write(ctx, name, index, 11, 22) },
			entry: func(e *p4v1.Entity) (uint32, *p4v1.Index) {
				return e.GetCounterEntry().GetCounterId(), e.GetCounterEntry().GetIndex()
			},
			id: 0x12000001,
		}
	case "meter":
		r, err := meter.NewReader(c, p)
		require.NoError(t, err)
		return indexedResource{
			read: func(ctx context.Context, name string, index int64) error {
				_, err := r.Read(ctx, name, index)
				return err
			},
			write: func(ctx context.Context, name string, index int64) error {
				return r.Write(ctx, name, index, meter.Config{CIR: 10, CBurst: 20, PIR: 30, PBurst: 40})
			},
			entry: func(e *p4v1.Entity) (uint32, *p4v1.Index) {
				return e.GetMeterEntry().GetMeterId(), e.GetMeterEntry().GetIndex()
			},
			id: 0x14000001,
		}
	default:
		r, err := register.NewReader(c, p)
		require.NoError(t, err)
		return indexedResource{
			read: func(ctx context.Context, name string, index int64) error {
				_, err := r.Read(ctx, name, index)
				return err
			},
			write: func(ctx context.Context, name string, index int64) error { return r.Write(ctx, name, index, []byte{1}) },
			entry: func(e *p4v1.Entity) (uint32, *p4v1.Index) {
				return e.GetRegisterEntry().GetRegisterId(), e.GetRegisterEntry().GetIndex()
			},
			id: 0x16000001,
		}
	}
}

func TestResourceIndexBoundaries(t *testing.T) {
	for _, kind := range []string{"counter", "meter", "register"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			h := testutil.StartServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.BecomePrimary(ctx))
			r := newIndexedResource(t, kind, c, 4, "")
			for _, tc := range []struct {
				name            string
				index           int64
				readOK, writeOK bool
			}{
				{"wildcard", -1, true, false},
				{"other negative", -2, false, false},
				{"minimum integer", math.MinInt64, false, false},
				{"first", 0, true, true},
				{"last", 3, true, true},
				{"size", 4, false, false},
				{"above size", 5, false, false},
				{"maximum integer", math.MaxInt64, false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					h.Mu.Lock()
					reads, writes := len(h.ReadRequests), len(h.WriteRequests)
					h.Mu.Unlock()
					readErr := r.read(ctx, kind, tc.index)
					writeErr := r.write(ctx, kind, tc.index)
					h.Mu.Lock()
					readRequests := append([]*p4v1.ReadRequest(nil), h.ReadRequests[reads:]...)
					writeRequests := append([]*p4v1.WriteRequest(nil), h.WriteRequests[writes:]...)
					h.Mu.Unlock()
					if tc.readOK {
						require.NoError(t, readErr)
						require.Len(t, readRequests, 1)
						require.Len(t, readRequests[0].Entities, 1)
						id, index := r.entry(readRequests[0].Entities[0])
						require.Equal(t, r.id, id)
						if tc.index == -1 {
							require.Nil(t, index)
						} else {
							require.NotNil(t, index)
							require.Equal(t, tc.index, index.Index)
						}
					} else {
						require.ErrorContains(t, readErr, "index")
						require.Empty(t, readRequests, "invalid read reached target")
					}
					if tc.writeOK {
						require.NoError(t, writeErr)
						require.Len(t, writeRequests, 1)
						require.Len(t, writeRequests[0].Updates, 1)
						require.Equal(t, p4v1.Update_MODIFY, writeRequests[0].Updates[0].Type)
						id, index := r.entry(writeRequests[0].Updates[0].Entity)
						require.Equal(t, r.id, id)
						require.NotNil(t, index)
						require.Equal(t, tc.index, index.Index)
					} else {
						require.ErrorContains(t, writeErr, "index")
						require.Empty(t, writeRequests, "invalid write reached target")
					}
				})
			}
			t.Run("alias", func(t *testing.T) {
				require.NoError(t, r.read(ctx, kind+"_alias", 3))
				require.NoError(t, r.write(ctx, kind+"_alias", 3))
				require.ErrorContains(t, r.read(ctx, kind+"_alias", 4), "index")
				require.ErrorContains(t, r.write(ctx, kind+"_alias", 4), "index")
			})
			sizes := []int64{1, math.MaxInt32}
			if kind != "register" {
				sizes = append(sizes, math.MaxInt64)
			}
			for _, size := range sizes {
				large := newIndexedResource(t, kind, c, size, "")
				require.NoError(t, large.read(ctx, kind, size-1))
				require.NoError(t, large.write(ctx, kind, size-1))
				require.ErrorContains(t, large.read(ctx, kind, size), "index")
				require.ErrorContains(t, large.write(ctx, kind, size), "index")
			}
			for _, size := range []int64{0, -1} {
				small := newIndexedResource(t, kind, c, size, "")
				require.Error(t, small.read(ctx, kind, 0))
				require.Error(t, small.write(ctx, kind, 0))
			}
			for _, size := range []int64{0, 4} {
				translated := newIndexedResource(t, kind, c, size, "PortId_t")
				for _, index := range []int64{0, 4, 100000, math.MaxInt64} {
					require.NoError(t, translated.read(ctx, kind, index))
					require.NoError(t, translated.write(ctx, kind, index))
					h.Mu.Lock()
					readRequest := h.ReadRequests[len(h.ReadRequests)-1]
					writeRequest := h.WriteRequests[len(h.WriteRequests)-1]
					h.Mu.Unlock()
					_, readIndex := translated.entry(readRequest.Entities[0])
					_, writeIndex := translated.entry(writeRequest.Updates[0].Entity)
					require.NotNil(t, readIndex)
					require.NotNil(t, writeIndex)
					require.Equal(t, index, readIndex.Index)
					require.Equal(t, index, writeIndex.Index)
				}
				require.NoError(t, translated.read(ctx, kind, -1))
				require.ErrorContains(t, translated.read(ctx, kind, -2), "index")
				require.ErrorContains(t, translated.write(ctx, kind, -1), "index")
			}
		})
	}
}
