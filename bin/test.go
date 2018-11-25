package main

import (
	"fmt"

	"github.com/jhead/mon/pkg/trace"
)

func main() {
	fmt.Println("Starting server")
	server, err := trace.New()
	if err != nil {
		panic(err)
	}

	fmt.Println("Sending pings")
	err = server.Ping("1.1.1.1")
	if err != nil {
		panic(err)
	}
}
