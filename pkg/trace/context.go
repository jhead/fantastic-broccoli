package trace

import (
	"fmt"
	"time"
)

func (ctx *TraceContext) handlePingRequest(req PingRequest) {
	fmt.Printf("Got request: %v @ %d\n", req.Target, req.TTL)

	// Set sequence num
	req.Seq = ctx.seq

	// Place into awaiting reply queue
	ctx.awaitingReply[ReplyKey{
		Target: formatAddress(req.Target),
		Seq:    req.Seq,
	}] = req

	// Increment sequence for next request
	ctx.seq++

	// Perform ping
	err := sendPing(ctx.conn, req)
	if err != nil {
		panic(err)
	}
}

func (ctx *TraceContext) handlePingReply(reply PingReply) {
	key := reply.key

	request, ok := ctx.awaitingReply[key]
	if !ok {
		fmt.Printf("Unrecognized key: %v\n", reply)
		request.Notify <- PingResult{
			Target: reply.peer,
			RTT:    0,
		}
		return
	}

	delete(ctx.awaitingReply, key)
	rtt := time.Since(request.timeSent)

	result := PingResult{
		Target: reply.peer,
		RTT:    uint64(rtt.Nanoseconds()),
	}

	request.Notify <- result
}
