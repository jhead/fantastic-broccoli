package trace

import (
	"net"
	"time"

	"golang.org/x/net/icmp"
)

const (
	ProtocolICMP = 1
	//ProtocolIPv6ICMP = 58
	ListenAddr     = "0.0.0.0"
	Network        = "udp4"
	MaxMessageSize = 1500
	MaxTTL         = 60
)

type internalMessage interface{}

type TraceServer struct {
	context *TraceContext
	dead    bool
	queue   chan internalMessage
}

type TraceContext struct {
	awaitingReply map[ReplyKey]PingRequest
	conn          *icmp.PacketConn
	seq           uint16
}

type PingRequest struct {
	Notify   chan PingResult
	Target   net.Addr
	TTL      uint8
	Seq      uint16
	timeSent time.Time
}

type PingResult struct {
	Target net.Addr
	RTT    uint64
}

type AggregatePingResult struct {
	Target  net.Addr
	MeanRTT uint64
	MinRTT  uint64
	MaxRTT  uint64
	Count   int
	Lost    int
}

type PingReply struct {
	peer net.Addr
	key  ReplyKey
}

type ReplyKey struct {
	Target string
	Seq    uint16
}
