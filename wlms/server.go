package main

import (
	"container/list"
	"io"
	"log"
	"net"
	"time"
)

type Server struct {
	acceptedConnections  chan io.ReadWriteCloser
	shutdownServer       chan bool
	serverHasShutdown    chan bool
	clients              *list.List
	user_db              UserDb
	motd                 string
	clientSendingTimeout time.Duration
	pingCycleTime        time.Duration
}

func (s *Server) SetClientSendingTimeout(d time.Duration) {
	s.clientSendingTimeout = d
}
func (s Server) ClientSendingTimeout() time.Duration {
	return s.clientSendingTimeout
}

func (s *Server) SetPingCycleTime(d time.Duration) {
	s.pingCycleTime = d
}
func (s Server) PingCycleTime() time.Duration {
	return s.pingCycleTime
}

func (s Server) Motd() string {
	return s.motd
}
func (s *Server) SetMotd(v string) {
	s.motd = v
}

func (s Server) UserDb() UserDb {
	return s.user_db
}

func (s *Server) Shutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) NrClients() int {
	return s.clients.Len()
}

func (s *Server) isLoggedIn(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name {
			return client
		}
	}
	return nil
}

func (s *Server) mainLoop() error {
	log.Print("Starting Goroutine: mainLoop")
	for done := false; !done; {
		select {
		case conn, ok := <-s.acceptedConnections:
			if !ok {
				done = true
			} else {
				// The client will register itself if it feels the need.
				go DealWithNewConnection(conn, s)
			}
		case <-s.shutdownServer:
			for s.clients.Len() > 0 {
				e := s.clients.Front()
				e.Value.(*Client).Disconnect()
				s.clients.Remove(e)
			}
			close(s.acceptedConnections)
			s.serverHasShutdown <- true
			done = true
		}
	}
	log.Print("Ending Goroutine: mainLoop")

	return nil
}

func (s *Server) shutdown() error {
	return nil
}

func (s *Server) AddClient(client *Client) {
	s.clients.PushBack(client)
}

func (s *Server) RemoveClient(client *Client) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			s.clients.Remove(e)
			s.broadcastToConnectedClients("CLIENTS_UPDATE")
		}
	}
}

func (s *Server) broadcastToConnectedClients(data ...interface{}) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.State() == CONNECTED {
			client.SendPacket(data...)
		}
	}
}

func listeningLoop(C chan io.ReadWriteCloser) {
	ln, err := net.Listen("tcp", ":7395") // TODO(sirver): softcode this
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			break
		}
		C <- conn
	}
}
func CreateServer() *Server {
	// NOCOM(sirver): should use a proper database connection or flat file
	C := make(chan io.ReadWriteCloser)
	// NOCOM(sirver): no way to stop the listening loop right now
	go listeningLoop(C)
	return CreateServerUsing(C, NewInMemoryDb())
}

func CreateServerUsing(acceptedConnections chan io.ReadWriteCloser, db UserDb) *Server {
	server := &Server{
		acceptedConnections:  acceptedConnections,
		shutdownServer:       make(chan bool),
		serverHasShutdown:    make(chan bool),
		clients:              list.New(),
		user_db:              db,
		clientSendingTimeout: time.Second * 30,
		pingCycleTime:        time.Second * 15,
	}

	go server.mainLoop()
	return server
}
