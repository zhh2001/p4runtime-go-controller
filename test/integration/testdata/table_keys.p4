#include <core.p4>
#include <v1model.p4>

struct headers_t {}
struct metadata_t {}

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
    action forward(bit<9> port) {
        s.egress_spec = port;
    }
    action entry_only(bit<9> port) {
        s.egress_spec = port;
    }
    action default_only(bit<9> port) {
        s.egress_spec = port;
    }
    table t_exact {
        key = {
            s.ingress_port : exact;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_lpm {
        key = {
            s.packet_length : lpm;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_ternary {
        key = {
            s.ingress_port : ternary;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_range {
        key = {
            s.ingress_port : range;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_optional {
        key = {
            s.ingress_port : optional;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_mixed {
        key = {
            s.ingress_port : exact;
            s.egress_spec : ternary;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = NoAction();
        size = 32;
    }
    table t_scoped {
        key = {
            s.ingress_port : exact;
        }
        actions = {
            forward;
            @tableonly entry_only;
            @defaultonly default_only;
        }
        default_action = default_only(1);
        support_timeout = true;
        size = 32;
    }
    table t_const_default {
        key = {
            s.ingress_port : exact;
        }
        actions = {
            forward;
            NoAction;
        }
        const default_action = forward(1);
        size = 32;
    }
    table t_constant {
        key = {
            s.ingress_port : exact;
        }
        actions = {
            forward;
            NoAction;
        }
        default_action = forward(1);
        const entries = {
            1 : forward(2);
        }
    }
    action_profile(32) profile;
    table t_indirect {
        key = {
            s.ingress_port : exact;
        }
        actions = {
            forward;
        }
        implementation = profile;
        size = 32;
    }
    apply {
        t_exact.apply();
        t_lpm.apply();
        t_ternary.apply();
        t_range.apply();
        t_optional.apply();
        t_mixed.apply();
        t_scoped.apply();
        t_const_default.apply();
        t_constant.apply();
        t_indirect.apply();
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
