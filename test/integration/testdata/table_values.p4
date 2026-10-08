#include <core.p4>
#include <v1model.p4>

struct headers_t {}
struct metadata_t {
    bit<128> key;
    bit<129> value;
}

parser MyParser(packet_in p, out headers_t h, inout metadata_t m,
                inout standard_metadata_t s) {
    state start {
        transition accept;
    }
}

control MyVerifyChecksum(inout headers_t h, inout metadata_t m) {
    apply {}
}

control MyComputeChecksum(inout headers_t h, inout metadata_t m) {
    apply {}
}

control MyIngress(inout headers_t h, inout metadata_t m,
                  inout standard_metadata_t s) {
    action store(bit<129> value, bit<9> port) {
        m.value = value;
        s.egress_spec = port;
    }
    table t_exact {
        key = {
            m.key : exact;
        }
        actions = {
            store;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_lpm {
        key = {
            m.key : lpm;
        }
        actions = {
            store;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_ternary {
        key = {
            m.key : ternary;
        }
        actions = {
            store;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_range {
        key = {
            m.key : range;
        }
        actions = {
            store;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_optional {
        key = {
            m.key : optional;
        }
        actions = {
            store;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    apply {
        m.key = 0;
        m.value = 0;
        t_exact.apply();
        t_lpm.apply();
        t_ternary.apply();
        t_range.apply();
        t_optional.apply();
        if (m.value != 0) {
            s.egress_spec = 1;
        }
    }
}

control MyEgress(inout headers_t h, inout metadata_t m,
                 inout standard_metadata_t s) {
    apply {}
}

control MyDeparser(packet_out p, in headers_t h) {
    apply {}
}

V1Switch(MyParser(),
         MyVerifyChecksum(),
         MyIngress(),
         MyEgress(),
         MyComputeChecksum(),
         MyDeparser()) main;
