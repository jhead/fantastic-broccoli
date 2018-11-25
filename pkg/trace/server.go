package trace

import (
	"fmt"
	"math"
	"net"
	"time"

	"golang.org/x/net/icmp"
)

func New() (*TraceServer, error) {
	server := &TraceServer{}

	conn, err := icmp.ListenPacket(Network, ListenAddr)
	if err != nil {
		return nil, err
	}

	server.queue = make(chan internalMessage)

	server.context = &TraceContext{
		conn:          conn,
		awaitingReply: make(map[ReplyKey]PingRequest),
	}

	go server.messageLoop()
	go server.receiveLoop()

	return server, nil
}

func (server *TraceServer) messageLoop() {
	fmt.Println("Started message loop")

	for !server.dead {
		fmt.Println("Polling for messages")
		msg := <-server.queue
		fmt.Printf("Got message %T: %v\n", msg, msg)

		switch typedMsg := msg.(type) {
		case PingRequest:
			server.context.handlePingRequest(typedMsg)
		case PingReply:
			server.context.handlePingReply(typedMsg)
		default:
			panic(typedMsg)
		}
	}
}

func (server *TraceServer) receiveLoop() {
	buffer := make([]byte, MaxMessageSize)

	for !server.dead {
		fmt.Println("Polling for new replies")
		server.queue <- receiveReply(server.context.conn, buffer)
	}
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

		if agg.Target != nil {
			if formatAddress(agg.Target) == formatAddress(targetAddr) {
				break
			}
		}

		ttl++
	}

	return nil
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

// func (server *TraceServer) cleanup() {
// 	timeout := 5 * time.Second

// 	for !server.dead {
// 		fmt.Println("Cleaning up")
// 		var removeList []ReplyKey

// 		for key, entry := range server.awaitingReply {
// 			if time.Since(entry.timeSent) > timeout {
// 				fmt.Printf("Timeout: %v %d\n", entry.Target, entry.TTL)
// 				removeList = append(removeList, key)
// 			}
// 		}

// 		// todo: this can't be safe
// 		for _, key := range removeList {
// 			delete(server.awaitingReply, key)
// 		}

// 		fmt.Printf("Deleted %d keys\n", len(removeList))
// 		time.Sleep(1 * time.Second)
// 	}
// }
