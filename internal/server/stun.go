package server

import (
	"encoding/binary"
	"errors"
	"net"
)

// A STUN responder (RFC 8489, the binding method only). Browsers in live mode ask it which
// public address they appear from, so that two of them can connect directly. Running it here
// means no third party (a public STUN service) learns who connects to whom.

const (
	stunMagic           = 0x2112A442
	stunBindingRequest  = 0x0001
	stunBindingResponse = 0x0101
	stunXorMappedAddr   = 0x0020
	stunHeaderLen       = 20
)

// ServeSTUN answers binding requests on conn until it is closed.
func ServeSTUN(conn net.PacketConn) error {
	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if resp := stunResponse(buf[:n], addr); resp != nil {
			_, _ = conn.WriteTo(resp, addr)
		}
	}
}

// stunResponse builds the answer to a well-formed binding request, and nil for anything else:
// a reply is never much larger than the request, so the responder is useless for amplification.
func stunResponse(req []byte, addr net.Addr) []byte {
	ua, ok := addr.(*net.UDPAddr)
	if !ok || len(req) < stunHeaderLen ||
		binary.BigEndian.Uint16(req[0:2]) != stunBindingRequest ||
		int(binary.BigEndian.Uint16(req[2:4])) != len(req)-stunHeaderLen ||
		binary.BigEndian.Uint32(req[4:8]) != stunMagic {
		return nil
	}
	txID := req[8:20]

	// XOR-MAPPED-ADDRESS: the port and address, masked with the magic cookie (and, for IPv6,
	// the transaction id) so that middleboxes rewriting addresses in packets leave it alone.
	var value []byte
	port := uint16(ua.Port) ^ uint16(stunMagic>>16)
	if ip4 := ua.IP.To4(); ip4 != nil {
		value = make([]byte, 8)
		value[1] = 0x01
		binary.BigEndian.PutUint16(value[2:4], port)
		binary.BigEndian.PutUint32(value[4:8], binary.BigEndian.Uint32(ip4)^stunMagic)
	} else {
		ip6 := ua.IP.To16()
		if ip6 == nil {
			return nil
		}
		value = make([]byte, 20)
		value[1] = 0x02
		binary.BigEndian.PutUint16(value[2:4], port)
		mask := binary.BigEndian.AppendUint32(nil, stunMagic)
		mask = append(mask, txID...)
		for i := range 16 {
			value[4+i] = ip6[i] ^ mask[i]
		}
	}

	resp := make([]byte, stunHeaderLen, stunHeaderLen+4+len(value))
	binary.BigEndian.PutUint16(resp[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], uint16(4+len(value)))
	binary.BigEndian.PutUint32(resp[4:8], stunMagic)
	copy(resp[8:20], txID)
	resp = binary.BigEndian.AppendUint16(resp, stunXorMappedAddr)
	resp = binary.BigEndian.AppendUint16(resp, uint16(len(value)))
	return append(resp, value...)
}
