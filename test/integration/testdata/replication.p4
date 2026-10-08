#include <core.p4>
#include <v1model.p4>

header ethernet_t {
    bit<48> dst;
    bit<48> src;
    bit<16> eth_type;
}
struct headers_t {
    ethernet_t eth;
}
struct metadata_t { @field_list(1) bit<1> clone_tag; }

parser MyParser(packet_in p, out headers_t h, inout metadata_t m,
                inout standard_metadata_t s) {
    state start {
        p.extract(h.eth);
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
    apply {
        m.clone_tag = 0;
        if (h.eth.eth_type == 0x88b5) {
            if (h.eth.dst[0:0] == 0) {
                s.mcast_grp = 19;
            } else {
                m.clone_tag = 1;
                clone_preserving_field_list(CloneType.I2E, 319, 1);
                mark_to_drop(s);
            }
        } else {
            mark_to_drop(s);
        }
    }
}

control MyEgress(inout headers_t h, inout metadata_t m,
                 inout standard_metadata_t s) {
    apply {
        h.eth.src[7:0] = (bit<8>)s.egress_rid;
    }
}

control MyDeparser(packet_out p, in headers_t h) {
    apply {
        p.emit(h.eth);
    }
}

V1Switch(MyParser(),
         MyVerifyChecksum(),
         MyIngress(),
         MyEgress(),
         MyComputeChecksum(),
         MyDeparser()) main;
