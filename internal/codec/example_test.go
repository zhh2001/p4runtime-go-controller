package codec_test

import (
	"fmt"

	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func ExampleEncodeUint() {
	b, _ := codec.EncodeUint(0xdeadbeef, 32)
	fmt.Printf("%x", b)
	// Output: deadbeef
}

func ExampleMAC() {
	b, _ := codec.MAC("aa:bb:cc:dd:ee:ff")
	fmt.Printf("len=%d %x", len(b), b)
	// Output: len=6 aabbccddeeff
}
