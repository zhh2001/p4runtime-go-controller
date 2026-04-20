package codec_test

import (
	"testing"

	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func BenchmarkEncodeUint(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = codec.EncodeUint(uint64(i), 32)
	}
}

func BenchmarkLPMMask(b *testing.B) {
	v, _ := codec.IPv4("10.1.2.3")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = codec.LPMMask(v, 24, 32)
	}
}

func BenchmarkEncodeBytes(b *testing.B) {
	in := []byte{0x00, 0x00, 0xab, 0xcd}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = codec.EncodeBytes(in, 32)
	}
}
