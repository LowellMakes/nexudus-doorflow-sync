package main

import (
    "github.com/nats-io/nats.go"
    "log"
)

func main() {
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Panicln("Could not connect to nats:", err)
	}
	defer nc.Close()
}
