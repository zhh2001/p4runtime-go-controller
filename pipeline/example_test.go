package pipeline_test

import (
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"

	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func ExampleNew() {
	info := &p4configv1.P4Info{
		Tables: []*p4configv1.Table{{
			Preamble: &p4configv1.Preamble{Id: 10, Name: "ingress.t"},
			Size:     256,
		}},
	}
	p, _ := pipeline.New(info, nil)
	t, ok := p.Table("ingress.t")
	fmt.Printf("ok=%v id=%d", ok, t.ID)
	// Output: ok=true id=10
}
