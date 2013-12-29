package main

import (
	"container/list"
	"io"
	"log"
	"net"
	"time"
)

type ReadWriteCloserWithIp interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

type Server struct {
	acceptedConnections  chan ReadWriteCloserWithIp
	shutdownServer       chan bool
	serverHasShutdown    chan bool
	clients              *list.List
	games                *list.List
	user_db              UserDb
	motd                 string
	clientSendingTimeout time.Duration
	pingCycleTime        time.Duration
	gamePingTimeout      time.Duration
	clientForgetTimeout  time.Duration
	gamePingCreator      GamePingFactory
}

type GamePingFactory interface {
	New(client *Client, timeout time.Duration) *GamePinger
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

func (s Server) GamePingTimeout() time.Duration {
	return s.gamePingTimeout
}
func (s *Server) SetGamePingTimeout(v time.Duration) {
	s.gamePingTimeout = v
}

func (s Server) ClientForgetTimeout() time.Duration {
	return s.clientForgetTimeout
}
func (s *Server) SetClientForgetTimeout(v time.Duration) {
	s.clientForgetTimeout = v
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

// NOCOM(sirver): this is only good for testing.
func (s *Server) NewGamePinger(client *Client) *GamePinger {
	return s.gamePingCreator.New(client, s.gamePingTimeout)
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

func (s *Server) AddClient(client *Client) {
	s.clients.PushBack(client)
}

func (s *Server) RemoveClient(client *Client) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			s.clients.Remove(e)
			s.BroadcastToConnectedClients("CLIENTS_UPDATE")
		}
	}
}

func (s *Server) HasClient(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name {
			return client
		}
	}
	return nil
}

func (s *Server) NrActiveClients() int {
	count := 0
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client).State() == CONNECTED {
			count++
		}
	}
	return count
}

func (s Server) ForeachActiveClient(callback func(*Client)) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.State() != CONNECTED {
			continue
		}
		callback(client)
	}
}

func (s *Server) AddGame(game *Game) {
	s.games.PushBack(game)
	// NOCOM(sirver): do not broadcast implicitly
	s.BroadcastToConnectedClients("GAMES_UPDATE")
}

func (s *Server) RemoveGame(game *Game) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		if e.Value.(*Game) == game {
			s.games.Remove(e)
			s.BroadcastToConnectedClients("GAMES_UPDATE")
		}
	}
}

func (s *Server) HasGame(name string) *Game {
	for e := s.games.Front(); e != nil; e = e.Next() {
		game := e.Value.(*Game)
		if game.Name() == name {
			return game
		}
	}
	return nil
}

func (s *Server) NrGames() int {
	return s.games.Len()
}

func (s Server) ForeachGame(callback func(*Game)) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		callback(e.Value.(*Game))
	}
}

func (s *Server) BroadcastToConnectedClients(data ...interface{}) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.State() == CONNECTED {
			client.SendPacket(data...)
		}
	}
}

func listeningLoop(C chan ReadWriteCloserWithIp) {
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
	C := make(chan ReadWriteCloserWithIp)
	// NOCOM(sirver): no way to stop the listening loop right now
	go listeningLoop(C)

	return CreateServerUsing(C, NewInMemoryDb())
}

type RealGamePingFactory struct {
	server *Server
}

func (gpf RealGamePingFactory) New(client *Client, timeout time.Duration) *GamePinger {
	// NOCOM(sirver): need timing
	pinger := &GamePinger{
		make(chan bool),
	}

	go func() {
		conn, err := net.Dial("tcp", net.JoinHostPort(client.remoteIp(), "7396"))
		if err != nil {
			pinger.C <- false
			return
		}
		defer conn.Close()

		conn.SetDeadline(time.Now().Add(timeout))
		n, err := conn.Write([]byte(NETCMD_METASERVER_PING))
		if err != nil || n != len(NETCMD_METASERVER_PING) {
			pinger.C <- false
			return
		}

		conn.SetDeadline(time.Now().Add(timeout))
		data := make([]byte, len(NETCMD_METASERVER_PING))
		n, err = conn.Read(data)
		if err != nil || string(data) != NETCMD_METASERVER_PING {
			pinger.C <- false
			return
		}
		pinger.C <- true
	}()

	return pinger
}

func (server *Server) InjectGamePingCreator(gpf GamePingFactory) {
	server.gamePingCreator = gpf
}

func CreateServerUsing(acceptedConnections chan ReadWriteCloserWithIp, db UserDb) *Server {
	server := &Server{
		acceptedConnections:  acceptedConnections,
		shutdownServer:       make(chan bool),
		serverHasShutdown:    make(chan bool),
		clients:              list.New(),
		games:                list.New(),
		user_db:              db,
		clientSendingTimeout: time.Second * 30,
		pingCycleTime:        time.Second * 15,
		gamePingTimeout:      time.Second * 10,
		clientForgetTimeout:  time.Minute * 5,
	}
	server.gamePingCreator = RealGamePingFactory{server}

	go server.mainLoop()
	return server
}
