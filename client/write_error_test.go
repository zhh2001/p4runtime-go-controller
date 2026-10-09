package client

import (
	"errors"
	"fmt"
	"testing"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

func TestTranslateWriteError_InvalidDetails(t *testing.T) {
	duplicate, err := anypb.New(&p4v1.Error{CanonicalCode: int32(codes.AlreadyExists), Message: "duplicate"})
	require.NoError(t, err)
	success, err := anypb.New(&p4v1.Error{})
	require.NoError(t, err)
	other, err := anypb.New(wrapperspb.String("vendor response"))
	require.NoError(t, err)
	negative, err := anypb.New(&p4v1.Error{CanonicalCode: -1})
	require.NoError(t, err)
	unknownCode, err := anypb.New(&p4v1.Error{CanonicalCode: 17})
	require.NoError(t, err)
	cases := []struct {
		name    string
		code    codes.Code
		count   int
		details []*anypb.Any
	}{
		{"no details", codes.Unknown, 1, nil},
		{"too few", codes.Unknown, 2, []*anypb.Any{duplicate}},
		{"too many", codes.Unknown, 1, []*anypb.Any{duplicate, success}},
		{"foreign detail", codes.Unknown, 2, []*anypb.Any{duplicate, other}},
		{"nil detail", codes.Unknown, 2, []*anypb.Any{duplicate, nil}},
		{"corrupt detail", codes.Unknown, 2, []*anypb.Any{duplicate, {TypeUrl: duplicate.TypeUrl, Value: []byte{0xff}}}},
		{"negative code", codes.Unknown, 2, []*anypb.Any{duplicate, negative}},
		{"unknown code", codes.Unknown, 2, []*anypb.Any{duplicate, unknownCode}},
		{"all successful", codes.Unknown, 2, []*anypb.Any{success, success}},
		{"wrong RPC code", codes.Internal, 1, []*anypb.Any{duplicate}},
		{"zero updates", codes.Unknown, 0, []*anypb.Any{duplicate}},
		{"negative update count", codes.Unknown, -1, []*anypb.Any{duplicate}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := status.FromProto(&statuspb.Status{Code: int32(tc.code), Message: "write rejected", Details: tc.details}).Err()
			translated := translateWriteError(original, tc.count)
			var writeErr *errs.WriteError
			require.ErrorAs(t, translated, &writeErr)
			assert.Nil(t, writeErr.Updates)
			assert.ErrorIs(t, translated, original)
			assert.NotErrorIs(t, translated, errs.ErrEntryExists)
			assert.True(t, proto.Equal(status.Convert(original).Proto(), status.Convert(translated).Proto()))
		})
	}
}

func TestTranslateWriteError_PreservesPayload(t *testing.T) {
	payload, err := anypb.New(wrapperspb.String("vendor details"))
	require.NoError(t, err)
	detail := &p4v1.Error{CanonicalCode: int32(codes.AlreadyExists), Message: "duplicate key", Space: "vendor-arch-target", Code: 42, Details: payload}
	// Preserve fields unknown to this version of the SDK as well.
	detail.ProtoReflect().SetUnknown([]byte{0x30, 0x01})
	st, err := status.New(codes.Unknown, "batch failed").WithDetails(detail)
	require.NoError(t, err)
	original := st.Err()
	translated := translateWriteError(original, 1)
	var writeErr *errs.WriteError
	require.ErrorAs(t, translated, &writeErr)
	require.Len(t, writeErr.Updates, 1)
	assert.True(t, proto.Equal(detail, writeErr.Updates[0]))
	assert.True(t, proto.Equal(st.Proto(), writeErr.GRPCStatus().Proto()))
	assert.ErrorIs(t, translated, original)
	assert.ErrorIs(t, fmt.Errorf("operation: %w", translated), errs.ErrEntryExists)
	assert.Contains(t, translated.Error(), "update 0: AlreadyExists: duplicate key")
}

func TestTranslateWriteError_PreservesNonStatusError(t *testing.T) {
	original := errors.New("transport failed")
	assert.Same(t, original, translateWriteError(original, 1))
}

func FuzzTranslateWriteError(f *testing.F) {
	f.Add([]byte{0x08, 0x06, 0x12, 0x03, 'd', 'u', 'p'}, uint8(1))
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{0xff}, uint8(2))
	f.Fuzz(func(t *testing.T, payload []byte, count uint8) {
		detail := &anypb.Any{TypeUrl: "type.googleapis.com/p4.v1.Error", Value: payload}
		st := status.FromProto(&statuspb.Status{Code: int32(codes.Unknown), Message: "batch failed", Details: []*anypb.Any{detail}})
		original := st.Err()
		translated := translateWriteError(original, int(count))
		var writeErr *errs.WriteError
		require.ErrorAs(t, translated, &writeErr)
		assert.ErrorIs(t, translated, original)
		assert.True(t, proto.Equal(st.Proto(), status.Convert(translated).Proto()))
		if writeErr.Updates == nil {
			assert.NotErrorIs(t, translated, errs.ErrEntryExists)
			assert.NotErrorIs(t, translated, errs.ErrEntryNotFound)
		} else {
			require.Len(t, writeErr.Updates, int(count))
			code := writeErr.Updates[0].GetCanonicalCode()
			assert.Greater(t, code, int32(codes.OK))
			assert.LessOrEqual(t, code, int32(codes.Unauthenticated))
			assert.Equal(t, code == int32(codes.AlreadyExists), errors.Is(translated, errs.ErrEntryExists))
			assert.Equal(t, code == int32(codes.NotFound), errors.Is(translated, errs.ErrEntryNotFound))
		}
		assert.NotEmpty(t, translated.Error())
	})
}
