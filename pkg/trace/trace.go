package trace

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	// Stolen from https://godoc.org/golang.org/x/net/internal/iana
	// can't import "internal" packages
	ProtocolICMP = 1
	//ProtocolIPv6ICMP = 58
	ListenAddr     = "0.0.0.0"
	Network        = "ip4:icmp"
	MaxMessageSize = 1500
)

type TraceServer struct {
	BindAddress   string
	BindPort      uint16
	awaitingReply map[uint16]TraceRequest
	packetConn    *icmp.PacketConn
	dead          bool
	queue         chan TraceRequest
	seq           uint16
}

type TraceResult struct {
	Target net.Addr
	RTT    uint64
}

type TraceRequest struct {
	Notify   chan TraceResult
	Target   *net.IPAddr
	TTL      uint8
	Seq      uint16
	timeSent time.Time
}

type PingReply struct {
	Seq uint16
}

func (server *TraceServer) Start() error {
	packetConn, err := icmp.ListenPacket(Network, server.BindAddress)
	if err != nil {
		return err
	}

	server.packetConn = packetConn
	server.queue = make(chan TraceRequest, 1024)
	server.awaitingReply = make(map[uint16]TraceRequest)

	go server.receiveLoop()
	go server.sendLoop()

	return nil
}

func (server *TraceServer) sendLoop() {
	server.seq = 1

	for !server.dead {
		fmt.Println("Polling for new requests")

		// Wait for request
		req := <-server.queue

		fmt.Printf("Got request: %v\n", req)

		// Set sequence num
		req.Seq = server.seq

		// Place into awaiting reply queue
		server.awaitingReply[req.Seq] = req

		// Increment sequence for next request
		server.seq++

		// Perform ping
		err := server.ping(req)
		if err != nil {
			panic(err)
		}
	}
}

func (server *TraceServer) receiveLoop() {
	buffer := make([]byte, MaxMessageSize)

	for !server.dead {
		fmt.Println("Polling for new replies")
		server.receiveReply(buffer)
	}
}

func (server *TraceServer) receiveReply(buffer []byte) {
	bufferLen, peer, err := server.packetConn.ReadFrom(buffer)
	if err != nil {
		panic(err) // todo: log
		return
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
		return
	}

	var decoded PingReply
	switch replyMessage.Type {
	case ipv4.ICMPTypeEchoReply:
		decoded = decodePingReply(buffer)
	case ipv4.ICMPTypeTimeExceeded:
		decoded = decodePingTTLExeeded(buffer)
	default:
		panic(fmt.Sprintf("Unsupported ICMP reply type: %v", replyMessage.Type))
	}

	request, ok := server.awaitingReply[decoded.Seq]
	if !ok {
		fmt.Printf("Unrecognized seq: %d\n", decoded.Seq)
		request.Notify <- TraceResult{
			Target: peer,
			RTT:    0,
		}
		return
	}

	rtt := time.Since(request.timeSent)

	result := TraceResult{
		Target: peer,
		RTT:    uint64(rtt.Nanoseconds()),
	}

	request.Notify <- result
}

func decodePingReply(buffer []byte) PingReply {
	buffer = buffer[len(buffer)-2:]
	seq := binary.BigEndian.Uint16(buffer)
	return PingReply{seq}
}

func decodePingTTLExeeded(buffer []byte) PingReply {
	return decodePingReply(buffer)
}

func (server *TraceServer) ping(req TraceRequest) error {
	server.packetConn.IPv4PacketConn().SetTTL(int(req.TTL))

	// Make a new ICMP message
	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   0xfefe, // todo: what
			Seq:  int(req.Seq),
			Data: []byte(""),
		},
	}

	b, err := m.Marshal(nil)
	if err != nil {
		return err
	}

	n, err := server.packetConn.WriteTo(b, req.Target)
	if err != nil {
		return err
	} else if n != len(b) {
		return fmt.Errorf("got %v; want %v", n, len(b))
	}

	return nil
}

func (server *TraceServer) Ping(target string) error {
	// Resolve any DNS (if used) and get the real IP of the target
	targetAddr, err := net.ResolveIPAddr(Network, target)
	if err != nil {
		return err
	}

	var ttl uint8 = 3
	for {
		fmt.Printf("Sending ping to %v with TTL %d\n", target, ttl)
		reqNotify := make(chan TraceResult, 1)

		server.queue <- TraceRequest{
			Target: targetAddr,
			TTL:    ttl,
			Notify: reqNotify,
		}

		fmt.Println("Waiting for ping reply")
		<-reqNotify

		ttl++
	}
}
