package server

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func bindingRequest(txID []byte) []byte {
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], stunBindingRequest)
	binary.BigEndian.PutUint32(req[4:8], stunMagic)
	copy(req[8:], txID)
	return req
}

func TestSTUNAnswersWithTheMappedAddress(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go ServeSTUN(conn)
	t.Cleanup(func() { conn.Close() })

	client, err := net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	txID := []byte("0123456789ab")
	if _, err := client.Write(bindingRequest(txID)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp := make([]byte, 100)
	n, err := client.Read(resp)
	if err != nil {
		t.Fatal(err)
	}
	resp = resp[:n]
	if binary.BigEndian.Uint16(resp[0:2]) != stunBindingResponse || !bytes.Equal(resp[8:20], txID) || n != 32 {
		t.Fatalf("response %x", resp)
	}
	port := binary.BigEndian.Uint16(resp[26:28]) ^ uint16(stunMagic>>16)
	ip := binary.BigEndian.Uint32(resp[28:32]) ^ stunMagic
	me := client.LocalAddr().(*net.UDPAddr)
	if int(port) != me.Port || ip != binary.BigEndian.Uint32(me.IP.To4()) {
		t.Errorf("mapped %d.%d.%d.%d:%d, want %v", byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip), port, me)
	}
}

func TestSTUNIgnoresEverythingElse(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}
	good := bindingRequest([]byte("abcdefghijkl"))
	if stunResponse(good, addr) == nil {
		t.Fatal("a valid request got no answer")
	}
	wrongType := bytes.Clone(good)
	wrongType[1] = 0x11
	wrongCookie := bytes.Clone(good)
	wrongCookie[4] = 0
	wrongLength := append(bytes.Clone(good), 0, 0, 0, 0)
	for name, req := range map[string][]byte{"short": good[:19], "type": wrongType, "cookie": wrongCookie, "length": wrongLength, "garbage": []byte("GET / HTTP/1.1\r\n\r\n....")} {
		if stunResponse(req, addr) != nil {
			t.Errorf("%s: answered", name)
		}
	}
	v6 := stunResponse(good, &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 5000})
	if len(v6) != 20+4+20 || v6[25] != 0x02 {
		t.Errorf("IPv6 answer %x", v6)
	}
}
