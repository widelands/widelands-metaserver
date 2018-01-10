package main

import (
	"container/list"
	"github.com/widelands/widelands_metaserver/wlnr/rpc_data"
	"io"
	"log"
	"net"
	"net/rpc"
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
	irc                 *IRCBridgerChannels
	relay               *rpc.Client
	// The IP addresses of the wlnr instance
	relay_address AddressPair
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
		log.Printf("Warning: %s is in the client list %d times", client.Name(), cnt)
	}

	// Now remove the client for good if it is around.
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			log.Printf("Removing client %s", client.Name())
			s.clients.Remove(e)
			s.irc.messagesToIRC <- Message{
				message: client.Name() + " has left the lobby",
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
			log.Printf("Removing game '%s'", game.Name())
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
	case s.irc.messagesToIRC <- Message{
		message: message,
	}:
	default:
		log.Println("Message Queue full")
	}

}

// Mini class for RPC to receive messages
type ServerRPC struct {
	server *Server
}

func NewServerRPC(server *Server) *ServerRPC {
	return &ServerRPC{
		server: server,
	}
}

func (rpc *ServerRPC) GameConnected(in *rpc_data.NewGameData, response *bool) (err error) {
	rpc.server.RelayGameConnected(in.Name)
	return nil
}

func (rpc *ServerRPC) GameClosed(in *rpc_data.NewGameData, response *bool) (err error) {
	rpc.server.RelayGameClosed(in.Name)
	return nil
}

// End mini RPC class

func RunServer(db UserDb, irc *IRCBridgerChannels) {
	ln, err := net.Listen("tcp", ":7395")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	// Open connection to relay server
	connection, err := net.DialTimeout("tcp", "127.0.0.1:7398", time.Duration(10)*time.Second)
	if err != nil {
		log.Fatal("Unable to connect to relay server: ", err)
		return
	}
	relay := rpc.NewClient(connection)
	log.Println("Connected to relay server")

	// Open our rpc server
	rpcLn, err := net.Listen("tcp", ":7399")
	if err != nil {
		log.Fatal("Error when listening for RPC calls: ", err)
		return
	}
	defer rpcLn.Close()

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

	server := CreateServerUsing(C, db, irc, relay)

	// Run our rpc server
	rpc.Register(NewServerRPC(server))
	go rpc.Accept(rpcLn)

	server.WaitTillShutdown()
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

// Tells the relay server to start a game with the given name
// The host position in the game is protected by the given password
func (server *Server) RelayCreateGame(name string, hostPassword string) bool {
	// Tell relay to host game
	var success bool
	data := rpc_data.NewGameData{
		Name:     name,
		Password: hostPassword,
	}
	err := server.relay.Call("RelayRPC.NewGame", data, &success)
	if err != nil || success == false {
		log.Println("ERROR: Unable to create a game on the relay server. This should not happen")
		return false
	}
	return true
}

// The relay informs us that the game with the given name has been connected by the host
func (server *Server) RelayGameConnected(name string) {
	log.Printf("Relay notifies us that the host connected to its game '%s'", name)
	game := server.HasGame(name)
	if game == nil {
		log.Printf(" Game '%s' is unknown, might already been closed", name)
		return
	}
	game.SetState(*server, CONNECTABLE_BOTH)
}

// The relay informs us that the game with the given name has been closed
func (server *Server) RelayGameClosed(name string) {
	log.Printf("Relay notifies us that the game '%s' has been closed", name)
	game := server.HasGame(name)
	if game == nil {
		// The game might have already been deleted when the host has notified us about its end
		return
	}
	server.RemoveGame(game)
}

func (server *Server) GetRelayAddresses() AddressPair {
	return server.relay_address
}

func CreateServerUsing(acceptedConnections chan ReadWriteCloserWithIp, db UserDb, irc *IRCBridgerChannels, relay *rpc.Client) *Server {
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
		irc:                    irc,
		relay:                  relay,
		relay_address:          AddressPair{"", ""},
	}
	// Get the IP addresses of our domain
	// TODO(sirver): This should be configurable for testing. For now you need
	// to put 'localhost' here if you want to test Widelands locally against the
	// metaserver + relay.
	ips, err := net.LookupIP("widelands.org")
	if err != nil {
		log.Fatal("Failed to resolve own hostname")
		return nil
	}
	// Select one IPv4 and one IPv6 address and store them
	// Note: This program assumes that the server supports both IP versions
	for _, ip := range ips {
		if ip.To4() != nil {
			server.relay_address.ipv4 = ip.String()
			continue
		}
		server.relay_address.ipv6 = ip.String()
	}
	if server.relay_address.ipv4 == "" || server.relay_address.ipv6 == "" {
		log.Fatal("Could not get an IPv4 and an IPv6 address for own host")
		return nil
	}
	server.gamePingerFactory = RealGamePingerFactory{server}
	go func() {
		for {
			select {
			case m := <-irc.messagesFromIRC:
				server.BroadcastToConnectedClients("CHAT", m.nick, m.message, "public")
			case nick := <-irc.clientsJoiningIRC:
				client := NewIRCClient(nick)
				// Not using AddClient() since we don't want a broadcast to IRC here
				server.clients.PushBack(client)
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
			case nick := <-irc.clientsLeavingIRC:
				client := server.HasClient(nick)
				if client != nil {
					server.RemoveClient(client)
					server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				}
			}
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
