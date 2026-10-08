#include <core.p4>
#include <v1model.p4>

header ethernet_t {
    bit<48> dst;
    bit<48> src;
    bit<16> etherType;
}

struct headers_t { ethernet_t eth; }
struct metadata_t {}
struct mac_learn_t {
    bit<48> src;
    bit<9> ingress_port;
}
struct port_event_t {
    bit<9> ingress_port;
    bit<16> etherType;
}

parser MyParser(packet_in p, out headers_t hdr, inout metadata_t m,
                inout standard_metadata_t s) {
    state start {
        p.extract(hdr.eth);
        transition accept;
    }
}

control MyVerifyChecksum(inout headers_t hdr, inout metadata_t m) {
    apply {}
}

control MyIngress(inout headers_t hdr, inout metadata_t m,
                  inout standard_metadata_t s) {
    apply {
        if (hdr.eth.isValid()) {
            if (hdr.eth.dst[0:0] == 0) {
                digest<mac_learn_t>(1, { hdr.eth.src, s.ingress_port });
            } else {
                digest<port_event_t>(1, { s.ingress_port, hdr.eth.etherType });
            }
        }
        mark_to_drop(s);
    }
}

control MyEgress(inout headers_t hdr, inout metadata_t m,
                 inout standard_metadata_t s) {
    apply {}
}

control MyComputeChecksum(inout headers_t hdr, inout metadata_t m) {
    apply {}
}

control MyDeparser(packet_out p, in headers_t hdr) {
    apply { p.emit(hdr.eth); }
}

V1Switch(MyParser(), MyVerifyChecksum(), MyIngress(), MyEgress(),
         MyComputeChecksum(), MyDeparser()) main;
