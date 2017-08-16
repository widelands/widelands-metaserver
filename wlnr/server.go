package main

import (
	"container/list"
//	"io"
	"log"
	"net"
)

type Server struct {
	acceptedConnections  chan net.Conn
	shutdownServer       chan bool
	serverHasShutdown    chan bool
	games                *list.List
}

func (s *Server) InitiateShutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) CreateGame(name, password string) {
	log.Printf("creategame1\n")
	game := NewGame(name, password)
	s.games.PushBack(game)
}

func (s *Server) RemoveGame(game *Game) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		if e.Value.(*Game) == game {
			log.Printf("Removing game %s.", game.Name())
			s.games.Remove(e)
			return
		}
	}
	log.Printf("Error: Did not find game '%s' to remove!", game.Name())
}

func RunServer() {
	log.Printf("startserver1\n")
	// Port is up to discussion. A dynamic portnumber could be used, too
	ln, err := net.Listen("tcp", ":7397")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	C := make(chan net.Conn)
	go func() {
		for {
	log.Printf("waiting for accept\n")
			conn, err := ln.Accept()
	log.Printf("accepted something\n")
			if err != nil {
				break
			}
			C <- conn
		}
	}()

	server := &Server{
		acceptedConnections:    C,
		shutdownServer:         make(chan bool),
		serverHasShutdown:      make(chan bool),
		games:                  list.New(),
	}

	go server.mainLoop()

	log.Printf("startserver2\n")
// NOCOM Remove next line and create a better channel
server.CreateGame("mygame", "pwd")
	server.WaitTillShutdown()
	return
}

func (s *Server) mainLoop() {
	for {
	log.Printf("running mainloop\n")
		select {
		case conn, ok := <-s.acceptedConnections:
			if !ok {
				return
			}
			go s.dealWithNewConnection(New(conn))
		case <-s.shutdownServer:
			for s.games.Len() > 0 {
				e := s.games.Front()
				e.Value.(*Game).Shutdown()
				// Game removes itself
			}
			close(s.acceptedConnections)
			s.serverHasShutdown <- true
			return
		}
	}
}

func (s *Server) dealWithNewConnection(client *Client) {

	// TODO: Look into first packet and decide which game this is for
	// TODO: pass connection to matching game.
	cmd, error := client.ReadUint8()
	if error != nil || cmd != kHello {
		client.Disconnect("PROTOCOL_VIOLATION")
		return
	}
	version, error := client.ReadUint8()
	if error != nil {
		client.Disconnect("")
		return
	}
	name, error := client.ReadString()
	if error != nil {
		client.Disconnect("PROTOCOL_VIOLATION")
		return
	}
	password, error := client.ReadString()
	if error != nil {
		client.Disconnect("PROTOCOL_VIOLATION")
		return
	}
	// The game will handle the client
	for e := s.games.Front(); e != nil; e = e.Next() {
		game := e.Value.(*Game)
		if game.Name() == name {
			game.addClient(client, version, password)
			return
		}
	}
	// Matching game not found, close connection
	client.Disconnect("GAME_UNKNOWN")
}

