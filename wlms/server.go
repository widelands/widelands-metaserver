package main

import (
	"container/list"
	"fmt"
	"log"
	"net"
	"time"
)

// TODO(sirver): should not be constant
const MOTD string = "Welcome on the Widelands Server."

type Server struct {
	shutdownServer       chan bool
	serverHasShutdown    chan bool
	connectingClients    chan *Client
	disconnectingClients chan *Client
	clients              *list.List
	listener             net.Listener

	clientSendingTimeout time.Duration
	pingCycleTime        time.Duration
}

func (s *Server) Shutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) NrClients() int {
	log.Printf("s.clients.Len(): %v\n", s.clients.Len())
	return s.clients.Len()
}

func (s *Server) SetClientSendingTimeout(d time.Duration) {
	s.clientSendingTimeout = d
}

func (s *Server) SetPingCycleTime(d time.Duration) {
	s.pingCycleTime = d
}

func (s *Server) mainLoop() error {
	for done := false; !done; {
		select {
		case <-s.shutdownServer:
			// NOCOM(sirver): cleanup: disconnect
			s.shutdown()
			break
		case newClient := <-s.connectingClients:
			s.clients.PushBack(newClient)
			go s.dealWithClient(newClient)
		case disconnectingClient := <-s.disconnectingClients:
			log.Printf("disconnectingClient: %v\n", disconnectingClient)
			for e := s.clients.Front(); e != nil; e = e.Next() {
				if e.Value.(*Client) == disconnectingClient {
					s.clients.Remove(e)
				}
			}
		}
	}
	return nil
}

func (s *Server) shutdown() error {
	for s.clients.Len() > 0 {
		e := s.clients.Front()
		e.Value.(*Client).Disconnect()
		s.clients.Remove(e)
	}
	s.listener.Close()
	s.serverHasShutdown <- true
	return nil
}

func (s *Server) acceptLoop() {
	log.Print("Starting Goroutine: acceptLoop")
	for {
		conn, err := s.listener.Accept()
		log.Printf("conn: %v, err: %v\n", conn, err)
		if err != nil {
			log.Printf("Error accepting a connection: %v", err)
			break
		}
		s.connectingClients <- NewClient(conn)
	}
	log.Print("Ending Goroutine: acceptLoop")
}

func (s *Server) dealWithClient(client *Client) {
	log.Print("Starting Goroutine: dealWithClient")
	timeout_channel := make(chan bool)
	startToPingTimer := time.NewTimer(s.pingCycleTime)
	waitingForPong := false

	for done := false; !done; {
		time.AfterFunc(s.clientSendingTimeout, func() {
			timeout_channel <- true
		})
		select {
		case data, ok := <-client.DataStream:
			if !ok {
				done = true
			} else {
				// TODO(sirver): this should probably use some kind of map
				waitingForPong = false
				startToPingTimer.Reset(s.pingCycleTime)
				switch data {
				case "LOGIN":
					s.handleLOGIN(client)
				case "PONG":
					// do nothing
				default:
					fmt.Printf("Unsupported command %v\n", data)
					break
				}
			}
		case <-timeout_channel:
			// NOCOM(sirver): refactor into method
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.Disconnect()
			done = true
		case <-startToPingTimer.C:
			if waitingForPong {
				// NOCOM(sirver): refactor into method
				client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
				client.Disconnect()
				done = true
			}
			client.SendPacket("PING")
			waitingForPong = true
			startToPingTimer.Reset(s.pingCycleTime)
		}
	}
	log.Print("Ending Goroutine: dealWithClient")
	s.disconnectingClients <- client
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
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(Client)
		client.SendPacket(data...)
	}
}

func CreateServer() *Server {
	ln, err := net.Listen("tcp", ":7395") // TODO(sirver): softcode this
	if err != nil {
		log.Fatal(err)
	}
	return CreateServerUsing(ln)
}

func CreateServerUsing(l net.Listener) *Server {
	server := &Server{
		shutdownServer:       make(chan bool),
		serverHasShutdown:    make(chan bool),
		connectingClients:    make(chan *Client),
		disconnectingClients: make(chan *Client),
		clients:              list.New(),
		listener:             l,
		clientSendingTimeout: time.Second * 30,
		pingCycleTime:        time.Second * 15,
	}
	// NOCOM(sirver): check pingCycleTime < clientSendingTimeout
	go server.acceptLoop()
	go server.mainLoop()
	return server
}
