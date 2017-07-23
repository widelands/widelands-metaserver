package wlnr

import (
	"container/list"
	"io"
	"log"
	"net"
)

type ReadWriteCloserWithIp interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
	SendPacket()
}

func SendPacket(conn ReadWriteCloserWithIp, data ...interface{}) {
	conn.Write(packet.New(data...))
}

type Server struct {
	acceptedConnections  chan ReadWriteCloserWithIp
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

func StartServer() *Server {
	ln, err := net.Listen("tcp", ":7397")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	C := make(chan ReadWriteCloserWithIp)
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
		acceptedConnections:    acceptedConnections,
		shutdownServer:         make(chan bool),
		serverHasShutdown:      make(chan bool),
		games:                  list.New(),
	}

	go server.mainLoop()

	return server
}

func (s *Server) mainLoop() {
	for {
		select {
		case conn, ok := <-s.acceptedConnections:
			if !ok {
				return
			}
			go s.dealWithNewConnection(conn)
		case <-s.shutdownServer:
			for s.games.Len() > 0 {
				e := s.games.Front()
				e.Value.(*Game).Shutdown(*s)
				// Game removes itself
			}
			close(s.acceptedConnections)
			s.serverHasShutdown <- true
			return
		}
	}
}

func (s *Server) dealWithNewConnection(conn ReadWriteCloserWithIp) {
	// TODO: Look into first packet and decide which game this is for
	// TODO: pass connection to matching game.
	name:="game name"
	// The game will handle the client
	for e := s.games.Front(); e != nil; e = e.Next() {
		game := e.Value.(*Game)
		if game.Name() == name {
			game.addClient(conn)
			return
		}
	}
	// Matching game not found, close connection
	conn.SendPacket("DISCONNECT", "GAME_UNKOWN")
	conn.Close()
}

