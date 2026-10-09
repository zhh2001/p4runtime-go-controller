package codec_test

import (
	"testing"

	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func BenchmarkEncodeUint(b *testing.B) {
	for _, tc := range []struct {
		name  string
		value uint64
		width int
	}{
		{"zero", 0, 32},
		{"port", 511, 9},
		{"uint32", 1<<32 - 1, 32},
		{"uint64", ^uint64(0), 64},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := codec.EncodeUint(tc.value, tc.width); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLPMMask(b *testing.B) {
	v, err := codec.IPv4("10.1.2.3")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.LPMMask(v, 24, 32); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeBytes(b *testing.B) {
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{"canonical", []byte{0xab, 0xcd}},
		{"padded", []byte{0x00, 0x00, 0xab, 0xcd}},
		{"zero", []byte{0x00}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := codec.EncodeBytes(tc.value, 32); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
