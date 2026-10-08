#include <core.p4>
#include <v1model.p4>

struct headers_t {}
struct metadata_t {}

parser MyParser(packet_in p, out headers_t h, inout metadata_t m,
                inout standard_metadata_t s) {
    state start { transition accept; }
}

control MyVerifyChecksum(inout headers_t h, inout metadata_t m) { apply {} }
control MyComputeChecksum(inout headers_t h, inout metadata_t m) { apply {} }

control MyIngress(inout headers_t h, inout metadata_t m,
                  inout standard_metadata_t s) {
    action forward(bit<9> port) { s.egress_spec = port; }
    table t_lpm {
        key = { s.ingress_port : lpm; }
        actions = { forward; NoAction; }
        default_action = NoAction();
        size = 32;
    }
    table t_ternary {
        key = { s.ingress_port : ternary; }
        actions = { forward; NoAction; }
        default_action = NoAction();
        size = 32;
    }
    apply {
        t_lpm.apply();
        t_ternary.apply();
    }
}

control MyEgress(inout headers_t h, inout metadata_t m,
                 inout standard_metadata_t s) { apply {} }
control MyDeparser(packet_out p, in headers_t h) { apply {} }

V1Switch(MyParser(), MyVerifyChecksum(), MyIngress(), MyEgress(),
         MyComputeChecksum(), MyDeparser()) main;
