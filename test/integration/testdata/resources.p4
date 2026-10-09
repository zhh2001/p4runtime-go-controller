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
    counter(4, CounterType.packets_and_bytes) stats;
    meter(4, MeterType.packets) rate;
    register<bit<9>>(4) state;
    apply {
        bit<2> color;
        bit<9> value;
        stats.count(0);
        rate.execute_meter(0, color);
        state.read(value, 0);
        state.write(0, value);
        mark_to_drop(s);
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
