/*
 * Minimal illustrative L2 switch program.
 *
 * This is a reduced sample meant to document the P4Info layout the Go
 * examples expect. It compiles with p4c against the v1model architecture:
 *
 *   p4c --target bmv2 --arch v1model \
 *     --p4runtime-files l2.p4info.txt -o build l2.p4
 *
 * which produces `l2.bmv2.json` (device config) and `l2.p4info.txt`
 * (P4Info text proto).
 */

#include <core.p4>
#include <v1model.p4>

const bit<16> TYPE_IPV4 = 0x0800;

header ethernet_t {
    bit<48> dst;
    bit<48> src;
    bit<16> etherType;
}

struct headers_t { ethernet_t eth; }
struct metadata_t {}

@controller_header("packet_in")
header packet_in_header_t { bit<9> ingress_port; bit<7> _pad; }
@controller_header("packet_out")
header packet_out_header_t { bit<9> egress_port; bit<7> _pad; }

parser MyParser(packet_in p, out headers_t h, inout metadata_t m,
                inout standard_metadata_t s) {
    state start { p.extract(h.eth); transition accept; }
}

control MyVerifyChecksum(inout headers_t h, inout metadata_t m) { apply {} }
control MyComputeChecksum(inout headers_t h, inout metadata_t m) { apply {} }

control MyIngress(inout headers_t h, inout metadata_t m,
                  inout standard_metadata_t s) {
    direct_counter(CounterType.packets_and_bytes) pkt_counter;
    action drop() { mark_to_drop(s); }
    action forward(bit<9> port) {
        s.egress_spec = port;
        pkt_counter.count();
    }
    table t_l2 {
        key = { h.eth.dst : exact; }
        actions = { forward; drop; NoAction; }
        default_action = NoAction();
        counters = pkt_counter;
        size = 1024;
    }
    apply { t_l2.apply(); }
}

control MyEgress(inout headers_t h, inout metadata_t m,
                 inout standard_metadata_t s) { apply {} }

control MyDeparser(packet_out p, in headers_t h) { apply { p.emit(h.eth); } }

V1Switch(MyParser(), MyVerifyChecksum(), MyIngress(), MyEgress(),
         MyComputeChecksum(), MyDeparser()) main;
