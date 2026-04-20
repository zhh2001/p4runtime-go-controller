package tableentry_test

import (
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"

	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func ExampleBuilder() {
	info := &p4configv1.P4Info{
		Tables: []*p4configv1.Table{{
			Preamble: &p4configv1.Preamble{Id: 1, Name: "ingress.t_l2"},
			MatchFields: []*p4configv1.MatchField{{
				Id: 1, Name: "hdr.eth.dst", Bitwidth: 48,
				Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_EXACT},
			}},
			ActionRefs: []*p4configv1.ActionRef{{Id: 2}},
		}},
		Actions: []*p4configv1.Action{{
			Preamble: &p4configv1.Preamble{Id: 2, Name: "ingress.forward"},
			Params:   []*p4configv1.Action_Param{{Id: 1, Name: "port", Bitwidth: 9}},
		}},
	}
	p, _ := pipeline.New(info, nil)
	entry, _ := tableentry.NewBuilder(p, "ingress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("ingress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).
		Build()
	fmt.Printf("table_id=%d matches=%d", entry.TableId, len(entry.Match))
	// Output: table_id=1 matches=1
}
