package main

import (
	"log"
	"net"
)

type Listener struct {
	Quit       chan bool
	NewClients chan *Client

	l net.Listener
}

func (l *Listener) connectLoop() {
	log.Print("Starting Goroutine: connectLoop")
	for {
		conn, err := l.l.Accept()
		log.Printf("conn: %v, err: %v\n", conn, err)
		if err != nil {
			log.Printf("Error accepting a connection: %v", err)
			break
		}
		l.NewClients <- NewClient(conn)
	}
	log.Print("Ending Goroutine: connectLoop")
}

func CreateListenerUsing(listener net.Listener) *Listener {
	rv := &Listener{
		make(chan bool),
		make(chan *Client),
		listener}
	go rv.connectLoop()
	return rv
}

func CreateNetworkListener() *Listener {
	ln, err := net.Listen("tcp", ":7395") // TODO(sirver): softcode this
	if err != nil {
		log.Fatal(err)
	}
	return CreateListenerUsing(ln)
}
