package main

import (
	"fmt"
	"log"
	"time"
)

// TODO(sirver): should not be constant
const MOTD string = "Welcome on the Widelands Server."

type Server struct {
	ShutdownServer    chan bool
	ServerHasShutdown chan bool
	listener          *Listener

	clients []*Client
}

func (s *Server) runListeningLoop() error {
	for done := false; !done; {
		select {
		case <-s.ShutdownServer:
			s.ServerHasShutdown <- true
			break
		case newClient := <-s.listener.NewClients:
			s.clients = append(s.clients, newClient)
			go s.dealWithClient(newClient)
		}
	}
	return nil
}

func (s *Server) dealWithClient(client *Client) {
	log.Print("Starting Goroutine: dealWithClient")
	for {
		select {
		// TODO(sirver): this should probably use some kind of map
		case data := <-client.DataStream:
			switch data {
			case "LOGIN":
				s.handleLOGIN(client)
			default:
				fmt.Printf("Unsupported command %v\n", data)
				break
			}
		}
	}
	log.Print("Ending Goroutine: dealWithClient")
}

func (s *Server) handleLOGIN(client *Client) {
	// NOCOM(sirver): the data stream needs another abstraction
	// // TODO(sirver): error handling
	protocolVersion, _ := client.ExpectInt()
	userName, _ := client.ExpectString()
	buildId, _ := client.ExpectString()
	isRegisteredOnServer, _ := client.ExpectBool()
	fmt.Printf("protocolVersion: %v, userName: %v, buildId: %v, isRegisteredOnServer: %v\n", protocolVersion, userName, buildId, isRegisteredOnServer)

	client.SendPacket("LOGIN", userName, "UNREGISTERED")
	client.SetName(userName)

	loginTime := time.Now()
	client.SetLoginTime(loginTime)
	client.SendPacket("TIME", int(time.Now().Unix()))

	// client.SendPacket("CHAT", "", MOTD, "system")
}

func (s *Server) broadcastPacket(data ...interface{}) {
	for _, client := range s.clients {
		client.SendPacket(data...)
	}
}

func CreateServer() *Server {
	return CreateServerWithListener(CreateNetworkListener())
}

func CreateServerWithListener(l *Listener) *Server {
	server := &Server{
		make(chan bool),
		make(chan bool),
		l,
		make([]*Client, 0),
	}
	return server
}
