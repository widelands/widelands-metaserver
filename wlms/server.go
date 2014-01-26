package main

import (
	"container/list"
	"io"
	"log"
	"net"
	"time"
)

const NETCMD_METASERVER_PING = "\x00\x03@"

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
	ircbridge            IRCBridge
}

type GamePingFactory interface {
	New(client *Client, timeout time.Duration) *GamePinger
}

func (s Server) ClientSendingTimeout() time.Duration {
	return s.clientSendingTimeout
}
func (s *Server) SetClientSendingTimeout(d time.Duration) {
	s.clientSendingTimeout = d
}

func (s Server) PingCycleTime() time.Duration {
	return s.pingCycleTime
}
func (s *Server) SetPingCycleTime(d time.Duration) {
	s.pingCycleTime = d
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

func (s *Server) InitiateShutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) NewGamePinger(client *Client) *GamePinger {
	return s.gamePingCreator.New(client, s.gamePingTimeout)
}

func (s *Server) AddClient(client *Client) {
	s.ircbridge.send(client.Name() + " has joined the Metaserver lobby")
	s.clients.PushBack(client)
}

func (s *Server) RemoveClient(client *Client) {
	// Sanity check: make sure this user is not in our list of clients more than
	// once.
	cnt := 0
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client).Name() == client.Name() {
			cnt++
		}
	}
	if cnt > 1 {
		log.Printf("Warning: %s is in the client list %i times.", client.Name(), cnt)
	}

	// Now remove the client for good if it is around.
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			log.Printf("Removing client %s.", client.Name())
			s.clients.Remove(e)
			s.ircbridge.send(client.Name() + " has left the Metaserver lobby")
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
	s.BroadcastToConnectedClients("GAMES_UPDATE")
	s.ircbridge.send("A new game " + game.Name() + " was opened by " + game.Host())
}

func (s *Server) RemoveGame(game *Game) {
	for e := s.games.Front(); e != nil; e = e.Next() {
		if e.Value.(*Game) == game {
			log.Printf("Removing game %s.", game.Name())
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

func (s *Server) BroadcastToIRC(message string) {
	s.ircbridge.send(message)
}

func RunServer(db UserDb) {
	ln, err := net.Listen("tcp", ":7395")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	irc := NewIRCBridge()
	irc.connect()

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

	CreateServerUsing(C, db, *irc).WaitTillShutdown()
}

type RealGamePingFactory struct {
	server *Server
}

func (gpf RealGamePingFactory) New(client *Client, timeout time.Duration) *GamePinger {
	pinger := &GamePinger{make(chan bool)}

	data := make([]byte, len(NETCMD_METASERVER_PING))
	go func() {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(client.remoteIp(), "7396"), timeout)
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
		_, err = conn.Read(data)
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

func CreateServerUsing(acceptedConnections chan ReadWriteCloserWithIp, db UserDb, bridge IRCBridge) *Server {
	server := &Server{
		acceptedConnections:  acceptedConnections,
		shutdownServer:       make(chan bool),
		serverHasShutdown:    make(chan bool),
		clients:              list.New(),
		games:                list.New(),
		user_db:              db,
		gamePingTimeout:      time.Second * 5,
		pingCycleTime:        time.Second * 15,
		clientSendingTimeout: time.Minute * 2,
		clientForgetTimeout:  time.Minute * 5,
		ircbridge:            bridge,
	}
	server.gamePingCreator = RealGamePingFactory{server}
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
			// The client will register itself if it feels the need.
			go DealWithNewConnection(conn, s)
		case <-s.shutdownServer:
			for s.clients.Len() > 0 {
				e := s.clients.Front()
				e.Value.(*Client).Disconnect(*s)
				s.clients.Remove(e)
			}
			close(s.acceptedConnections)
			s.serverHasShutdown <- true
			return
		}
	}
}
