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

	fmt.Println("Sending pings")
	err = server.Ping("8.8.8.8")
	if err != nil {
		panic(err)
	}
}
