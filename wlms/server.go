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

	// Time in which a game has to respond to the first ping.
	gameInitialPingTimeout time.Duration

	// Time a game has to respond to all consecutive pings.
	gamePingTimeout time.Duration

	clientForgetTimeout time.Duration
	gamePingerFactory   GamePingerFactory
	messagesOut         chan Message
}

type GamePingerFactory interface {
	New(ip string, timeout time.Duration) *GamePinger
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

func (s Server) GameInitialPingTimeout() time.Duration {
	return s.gameInitialPingTimeout
}
func (s *Server) SetGameInitialPingTimeout(v time.Duration) {
	s.gameInitialPingTimeout = v
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

func (s *Server) NewGamePinger(ip string, ping_timeout time.Duration) *GamePinger {
	return s.gamePingerFactory.New(ip, ping_timeout)
}

func (s *Server) AddClient(client *Client) {
	s.BroadcastToIrc(client.Name() + " has joined the lobby.")
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
		log.Printf("Warning: %s is in the client list %d times.", client.Name(), cnt)
	}

	// Now remove the client for good if it is around.
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			log.Printf("Removing client %s.", client.Name())
			s.clients.Remove(e)
			s.messagesOut <- Message{
				message: client.Name() + " has left the lobby.",
				nick:    client.Name(),
			}
		}
	}
}

func (s Server) HasClient(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name {
			return client
		}
	}
	return nil
}

func (s Server) FindClientsToReplace(nonce string, name string) []*Client {
	res := make([]*Client, 0)
	var best *Client = nil

	// Assumptions: There is at most one client where nonce and name match,
	// there may be multiple clients with the same nonce
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.PendingLogin() != nil {
			// If there already is a replacement pending for this client, there is no use adding
			// it to this list. Either it will be replaced, or it is still active anyway
			continue
		}
		if client.Nonce() == nonce {
			// Nonce is the same so at least it is one connection of this player
			if client.Name() == name {
				// Even the wanted name? Great! Add it to the front of the list later on
				best = client
			} else {
				// Put disconnected clients on the front, (potentially) active ones in the back
				// This way, disconnected clients will be replaced first
				if client.State() == RECENTLY_DISCONNECTED {
					res = append([]*Client{client}, res...)
				} else {
					res = append(res, client)
				}
			}
		}
	}

	if best != nil {
		// If we found a perfect match, push to front
		res = append([]*Client{best}, res...)
	}
	return res
}

func (s Server) NrActiveClients() int {
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
	s.BroadcastToIrc("A new game " + game.Name() + " was opened by " + game.Host())
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

func (s Server) HasGame(name string) *Game {
	for e := s.games.Front(); e != nil; e = e.Next() {
		game := e.Value.(*Game)
		if game.Name() == name {
			return game
		}
	}
	return nil
}

func (s Server) NrGames() int {
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

func (s Server) BroadcastToIrc(message string) {
	select {
	case s.messagesOut <- Message{
		message: message,
	}:
	default:
		log.Println("Message Queue full.")
	}

}

func RunServer(db UserDb, messagesIn chan Message, messagesOut chan Message) {
	ln, err := net.Listen("tcp", ":7395")
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
	CreateServerUsing(C, db, messagesIn, messagesOut).WaitTillShutdown()
}

type RealGamePingerFactory struct {
	server *Server
}

func (gpf RealGamePingerFactory) New(ip string, timeout time.Duration) *GamePinger {
	pinger := &GamePinger{make(chan bool)}

	data := make([]byte, len(NETCMD_METASERVER_PING))
	go func() {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "7396"), timeout)
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

func (server *Server) InjectGamePingerFactory(gpf GamePingerFactory) {
	server.gamePingerFactory = gpf
}

func CreateServerUsing(acceptedConnections chan ReadWriteCloserWithIp, db UserDb, messagesIn chan Message, messagesOut chan Message) *Server {
	server := &Server{
		acceptedConnections:    acceptedConnections,
		shutdownServer:         make(chan bool),
		serverHasShutdown:      make(chan bool),
		clients:                list.New(),
		games:                  list.New(),
		user_db:                db,
		gameInitialPingTimeout: time.Second * 10,
		gamePingTimeout:        time.Second * 30,
		pingCycleTime:          time.Second * 15,
		clientSendingTimeout:   time.Minute * 2,
		clientForgetTimeout:    time.Minute * 5,
		messagesOut:            messagesOut,
	}
	server.gamePingerFactory = RealGamePingerFactory{server}
	go func() {
		for m := range messagesIn {
			server.BroadcastToConnectedClients("CHAT", m.nick+"(IRC)", m.message, "public")
		}
	}()
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
