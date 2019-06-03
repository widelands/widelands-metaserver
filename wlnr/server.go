package main

import (
	"container/list"
	"github.com/widelands/widelands_metaserver/wlnr/relayinterface"
	"log"
	"net"
)

type Server struct {
	acceptedConnections chan net.Conn
	shutdownServer      chan bool
	serverHasShutdown   chan bool
	games               *list.List
	wlms                relayinterface.Server
}

func (s *Server) InitiateShutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) CreateGame(name, password string) bool {

	// Check if the game already exists
	for e := s.games.Front(); e != nil; e = e.Next() {
		game := e.Value.(*Game)
		if game.Name() == name {
			log.Printf("Error: Ordered to create game '%v', but it already exists", name)
			return false
		}
	}
	// It does not, add it
	game := NewGame(name, password, s)
	log.Printf("Created game '%v'", name)
	s.games.PushBack(game)
	return true
}

func (s *Server) RemoveGame(name string) bool {
	for e := s.games.Front(); e != nil; e = e.Next() {
		g := e.Value.(*Game)
		if g.Name() == name {
			log.Printf("Removing game '%v' as told by metaserver", name)
			g.Shutdown()
			return true
		}
	}
	log.Printf("Error: Did not find game '%v' to remove as told by metaserver", name)
	return false
}

func (s *Server) GameConnected(name string) {
	s.wlms.GameConnected(name)
}

// Search for a game with the given name. If it exists but no host is connected, remove it
func (s *Server) RemoveGameIfNoHostIsConnected(name string) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		g := e.Value.(*Game)
		if g.Name() == name && g.host == nil {
			log.Printf("Removing game '%v' since no host connected to it", name)
			s.wlms.GameClosed(name)
			s.games.Remove(e)
			return
		}
	}
}

func (s *Server) RemoveGameObject(game *Game) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		if e.Value.(*Game) == game {
			s.wlms.GameClosed(game.Name())
			s.games.Remove(e)
			return
		}
	}
	log.Printf("Error: Did not find game '%v' to remove!", game.Name())
}

func RunServer() {
	ln, err := net.Listen("tcp", ":7397")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	C := make(chan net.Conn)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			C <- conn
		}
	}()

	server := &Server{
		acceptedConnections: C,
		shutdownServer:      make(chan bool),
		serverHasShutdown:   make(chan bool),
		games:               list.New(),
		wlms:                nil,
	}
	server.wlms = relayinterface.NewServerRPC(server)
	defer server.wlms.CloseConnection()

	go server.mainLoop()

	log.Println("The client ids are only unique within one game. Id=1 is host")

	server.WaitTillShutdown()
	return
}

func (s *Server) mainLoop() {
	for {
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
	cmd, error := client.ReadUint8()
	if error != nil || cmd != kHello {
		client.Disconnect("PROTOCOL_VIOLATION")
		return
	}
	version, error := client.ReadUint8()
	if error != nil {
		client.Disconnect("PROTOCOL_VIOLATION")
		return
	}
	if version != kRelayProtocolVersion {
		client.Disconnect("WRONG_VERSION")
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
