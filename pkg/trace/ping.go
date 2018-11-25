package trace

import (
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func receiveReply(conn *icmp.PacketConn, buffer []byte) PingReply {
	bufferLen, peer, err := conn.ReadFrom(buffer)
	if err != nil {
		panic(err) // todo: log
	}

	// 20 IP header + 8 ICMP
	// todo: ipv6
	if bufferLen < 8 {
		panic(fmt.Sprintf("Invalid ICMP reply length: %d", bufferLen))
	}

	// Adjust buffer to correct size
	buffer = buffer[:bufferLen]

	replyMessage, err := icmp.ParseMessage(ProtocolICMP, buffer)
	if err != nil {
		panic(err) // todo: log
	}

	var reply ReplyKey
	switch parsedReplyMessage := replyMessage.Body.(type) {
	case *icmp.Echo:
		reply = decodePingReply(peer, parsedReplyMessage)
	case *icmp.TimeExceeded:
		reply = decodePingTTLExeeded(parsedReplyMessage)
	default:
		panic(fmt.Sprintf("Unsupported ICMP reply type: %v", replyMessage.Type))
	}

	return PingReply{peer, reply}
}

func decodePingReply(from net.Addr, msg *icmp.Echo) ReplyKey {
	return ReplyKey{
		Target: formatAddress(from),
		Seq:    uint16(msg.Seq),
	}
}

func decodePingTTLExeeded(msg *icmp.TimeExceeded) ReplyKey {
	buffer := msg.Data

	header, _ := icmp.ParseIPv4Header(buffer)
	originalTarget := &net.IPAddr{header.Dst, ""}

	// Slice off IP header to get original echo request
	buffer = buffer[20:]
	inner, _ := icmp.ParseMessage(ProtocolICMP, buffer)

	switch parsedInner := inner.Body.(type) {
	case *icmp.Echo:
		return decodePingReply(originalTarget, parsedInner)
	default:
		panic(parsedInner)
	}
}

func formatAddress(addr net.Addr) string {
	addrString := addr.String()

	switch addr.(type) {
	case *net.IPAddr, *net.UDPAddr:
		return strings.Replace(addrString, ":0", "", -1)
	}

	return addrString
}

func sendPing(conn *icmp.PacketConn, req PingRequest) error {
	conn.IPv4PacketConn().SetTTL(int(req.TTL))

	// Make a new ICMP message
	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   0xfefe, // todo: what
			Seq:  int(req.Seq),
			Data: []byte{0xfd, 0xfd, 0xfd, 0xfd},
		},
	}

	b, err := m.Marshal(nil)
	if err != nil {
		return err
	}

	n, err := conn.WriteTo(b, req.Target)
	if err != nil {
		return err
	} else if n != len(b) {
		return fmt.Errorf("got %v; want %v", n, len(b))
	}

	return nil
}

func pollForReply(target net.Addr, notify <-chan PingResult) *PingResult {
	fmt.Println("Waiting for ping reply")
	select {
	case reply := <-notify:
		fmt.Println(reply)
		return &reply
	case <-time.After(250 * time.Millisecond):
		// timeout
	}

	return nil
}
