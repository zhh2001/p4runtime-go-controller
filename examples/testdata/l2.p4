#include <core.p4>
#include <v1model.p4>

const bit<9> CPU_PORT = 255;

header ethernet_t {
    bit<48> dst;
    bit<48> src;
    bit<16> etherType;
}

@id(1)
@controller_header("packet_in")
header packet_in_header_t {
    bit<9> ingress_port;
    bit<7> _pad;
}

@id(2)
@controller_header("packet_out")
header packet_out_header_t {
    bit<9> egress_port;
    bit<7> _pad;
}

struct headers_t {
    packet_in_header_t packet_in;
    packet_out_header_t packet_out;
    ethernet_t eth;
}
struct metadata_t {}

parser MyParser(packet_in p, out headers_t hdr, inout metadata_t m,
                inout standard_metadata_t s) {
    state start {
        transition select(s.ingress_port) {
            CPU_PORT: parse_packet_out;
            default: parse_ethernet;
        }
    }
    state parse_packet_out {
        p.extract(hdr.packet_out);
        transition parse_ethernet;
    }
    state parse_ethernet {
        p.extract(hdr.eth);
        transition accept;
    }
}

control MyVerifyChecksum(inout headers_t hdr, inout metadata_t m) {
    apply {}
}

control MyComputeChecksum(inout headers_t hdr, inout metadata_t m) {
    apply {}
}

control MyIngress(inout headers_t hdr, inout metadata_t m,
                  inout standard_metadata_t s) {
    @id(1) counter(512, CounterType.packets_and_bytes) pkt_counter;
    @id(1) direct_counter(CounterType.packets_and_bytes) pkt_counter_direct;

    @id(4) action drop() { mark_to_drop(s); }
    @id(3) action forward(bit<9> port) {
        s.egress_spec = port;
        pkt_counter.count((bit<32>) port);
        pkt_counter_direct.count();
    }
    @id(5) action punt() { s.egress_spec = CPU_PORT; }

    @id(1) table t_l2 {
        key = { hdr.eth.dst : exact; }
        actions = { forward; drop; @defaultonly punt; }
        default_action = punt();
        counters = pkt_counter_direct;
        size = 1024;
    }
    apply {
        if (s.parser_error != error.NoError || !hdr.eth.isValid()) {
            drop();
        } else if (hdr.packet_out.isValid()) {
            s.egress_spec = hdr.packet_out.egress_port;
            hdr.packet_out.setInvalid();
        } else {
            t_l2.apply();
        }
    }
}

control MyEgress(inout headers_t hdr, inout metadata_t m,
                 inout standard_metadata_t s) {
    apply {
        if (s.egress_port == CPU_PORT) {
            hdr.packet_in.setValid();
            hdr.packet_in.ingress_port = s.ingress_port;
            hdr.packet_in._pad = 0;
        }
    }
}

control MyDeparser(packet_out p, in headers_t hdr) {
    apply {
        p.emit(hdr.packet_in);
        p.emit(hdr.eth);
    }
}

V1Switch(MyParser(),
         MyVerifyChecksum(),
         MyIngress(),
         MyEgress(),
         MyComputeChecksum(),
         MyDeparser()) main;
