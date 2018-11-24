package main

import (
	"fmt"

	"github.com/jhead/mon/pkg/trace"
)

func main() {
	server := trace.TraceServer{}

	fmt.Println("Starting server")
	err := server.Start()
	if err != nil {
		panic(err)
	}

	go server.Ping("1.1.1.1")

	fmt.Println("Sending pings")
	err = server.Ping("8.8.8.8")
	if err != nil {
		panic(err)
	}
}
