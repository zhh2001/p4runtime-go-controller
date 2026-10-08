// Package packetio exposes PacketIn and PacketOut APIs backed by the
// bidirectional StreamChannel. Metadata is packed and unpacked according to
// the P4Info controller_packet_metadata definitions.
// PacketOut requires every declared field, including explicit padding values.
// Unsigned metadata is validated and sent in P4Info declaration order.
package packetio
