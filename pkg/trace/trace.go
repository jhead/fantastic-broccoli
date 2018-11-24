package trace

import (
	"fmt"
	"math"
	"net"
	"strings"
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
	Network        = "udp4"
	MaxMessageSize = 1500
	MaxTTL         = 60
)

type internalMessage interface{}

type TraceServer struct {
	BindAddress   string
	BindPort      uint16
	awaitingReply map[ReplyKey]PingRequest
	packetConn    *icmp.PacketConn
	dead          bool
	queue         chan internalMessage
	seq           uint16
}

type PingResult struct {
	Target net.Addr
	RTT    uint64
}

type PingRequest struct {
	Notify   chan PingResult
	Target   net.Addr
	TTL      uint8
	Seq      uint16
	timeSent time.Time
}

type ReplyKey struct {
	Target string
	Seq    uint16
}

func (server *TraceServer) Start() error {
	packetConn, err := icmp.ListenPacket(Network, server.BindAddress)
	if err != nil {
		return err
	}

	server.packetConn = packetConn
	server.queue = make(chan internalMessage, 1024)
	server.awaitingReply = make(map[ReplyKey]PingRequest)

	go server.messageLoop()

	return nil
}

func (server *TraceServer) messageLoop() {
	for !server.dead {
		msg := <-server.queue

		switch typedMsg := msg.(type) {
		case PingRequest:
			handlePingRequest(typedMsg)
		default:
			panic(typedMsg)
		}
	}
}

func handlePingRequest(req PingRequest) {
	fmt.Printf("Got request: %v @ %d\n", req.Target, req.TTL)

	// Set sequence num
	req.Seq = server.seq

	// Place into awaiting reply queue
	server.awaitingReply[ReplyKey{
		Target: formatAddress(req.Target),
		Seq:    req.Seq,
	}] = req

	// Increment sequence for next request
	server.seq++

	// Perform ping
	err := server.ping(req)
	if err != nil {
		panic(err)
	}
}

func (server *TraceServer) receiveLoop() {
	buffer := make([]byte, MaxMessageSize)

	for !server.dead {
		fmt.Println("Polling for new replies")
		server.receiveReply(buffer)
	}
}

func (server *TraceServer) cleanup() {
	timeout := 5 * time.Second

	for !server.dead {
		fmt.Println("Cleaning up")
		var removeList []ReplyKey

		for key, entry := range server.awaitingReply {
			if time.Since(entry.timeSent) > timeout {
				fmt.Printf("Timeout: %v %d\n", entry.Target, entry.TTL)
				removeList = append(removeList, key)
			}
		}

		// todo: this can't be safe
		for _, key := range removeList {
			delete(server.awaitingReply, key)
		}

		fmt.Printf("Deleted %d keys\n", len(removeList))
		time.Sleep(1 * time.Second)
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

	var decoded ReplyKey
	switch parsedReplyMessage := replyMessage.Body.(type) {
	case *icmp.Echo:
		decoded = decodePingReply(peer, parsedReplyMessage)
	case *icmp.TimeExceeded:
		decoded = decodePingTTLExeeded(parsedReplyMessage)
	default:
		panic(fmt.Sprintf("Unsupported ICMP reply type: %v", replyMessage.Type))
	}

	request, ok := server.awaitingReply[decoded]

	if !ok {
		fmt.Printf("Unrecognized key: %v\n", decoded)
		request.Notify <- PingResult{
			Target: peer,
			RTT:    0,
		}
		return
	}

	delete(server.awaitingReply, decoded)
	rtt := time.Since(request.timeSent)

	result := PingResult{
		Target: peer,
		RTT:    uint64(rtt.Nanoseconds()),
	}

	request.Notify <- result
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

func (server *TraceServer) ping(req PingRequest) error {
	server.packetConn.IPv4PacketConn().SetTTL(int(req.TTL))

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
	targetAddr, err := net.ResolveUDPAddr(Network, fmt.Sprintf("%s:0", target))
	if err != nil {
		return err
	}

	var ttl uint8 = 1

	for ttl < MaxTTL {
		agg := server.pingMultiple(targetAddr, ttl, 10)
		fmt.Println(agg)

		if formatAddress(agg.Target) == formatAddress(targetAddr) {
			break
		}

		ttl++
	}

	return nil
}

type AggregatePingResult struct {
	Target  net.Addr
	MeanRTT uint64
	MinRTT  uint64
	MaxRTT  uint64
	Count   int
	Lost    int
}

func (server *TraceServer) pingMultiple(target net.Addr, ttl uint8, n int) (agg AggregatePingResult) {
	var totalRTT uint64
	i := 0
	for i < n {
		fmt.Printf("Sending ping to %v with TTL %d\n", target, ttl)
		notify := make(chan PingResult, 1)

		server.queue <- PingRequest{
			Target:   target,
			TTL:      ttl,
			Notify:   notify,
			timeSent: time.Now(),
		}

		pingResult := pollForReply(target, notify)

		if pingResult == nil {
			fmt.Println("Lost")
			agg.Lost++
		} else {
			agg.Target = pingResult.Target

			agg.Count++
			totalRTT += pingResult.RTT
			agg.MeanRTT = totalRTT / uint64(agg.Count)

			if agg.MinRTT == 0 {
				agg.MinRTT = pingResult.RTT
			} else {
				agg.MinRTT = uint64(math.Min(float64(agg.MinRTT), float64(pingResult.RTT)))
			}

			agg.MaxRTT = uint64(math.Max(float64(agg.MaxRTT), float64(pingResult.RTT)))
		}

		i++
	}

	return
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
