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
struct metadata_t {}

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
    meter(4, MeterType.packets) rate;
    apply {
        bit<2> color;
        if (h.eth.eth_type == 0x88b5 && s.ingress_port == 1) {
            rate.execute_meter(0, color);
            h.eth.src[7:0] = (bit<8>)color;
            s.egress_spec = 2;
        } else {
            mark_to_drop(s);
        }
    }
}
control MyEgress(inout headers_t h, inout metadata_t m,
                 inout standard_metadata_t s) {
    apply {}
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
