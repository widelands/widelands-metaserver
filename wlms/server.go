package main

import (
	"container/list"
	"github.com/widelands/widelands_metaserver/wlnr/relayinterface"
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

type BannedIP struct {
	ip    string
	until time.Time
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
	relay               relayinterface.Client
	// The IP addresses of the wlnr instance
	relay_address AddressPair

	// A list of IPs that has been banned for some time
	banned *list.List
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
	if client.Permissions() != IRC {
		go client.Announce(*s)
	}
	s.clients.PushBack(client)
}

func (s *Server) RemoveClient(client *Client) {
	// Sanity check: make sure this user is not in our list of clients more than
	// once.
	cntGame := 0
	cntIRC := 0
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client).Name() == client.Name() {
			if e.Value.(*Client).buildId == "IRC" {
				cntIRC++
			} else {
				cntGame++
			}
		}
	}
	if cntIRC > 1 {
		log.Printf("Warning: IRC client %s is in the client list %d times", client.Name(), cntIRC)
	}
	if cntGame > 1 {
		log.Printf("Warning: Game client %s is in the client list %d times", client.Name(), cntGame)
	}

	// Now remove the client for good if it is around.
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			if client.Permissions() != IRC {
				log.Printf("Removing client %s", client.Name())
				go client.Announce(*s)
			}
			s.clients.Remove(e)
			break
		}
	}
}

func (s Server) HasClient(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name && client.buildId != "IRC" {
			return client
		}
	}
	return nil
}

func (s Server) HasClientObject(c *Client) bool {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client == c {
			return true
		}
	}
	return false
}

func (s Server) HasIRCClient(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name && client.buildId == "IRC" {
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

func (s Server) AddKickedClient(c *Client) {
	ip := c.conn.RemoteAddr().(*net.TCPAddr).IP.String()
	log.Printf("Kicking IP %v for 5 minutes", ip)
	s.banned.PushBack(&BannedIP{ip, time.Now().Add(5 * time.Minute)})
}

func (s Server) AddBannedClient(c *Client) {
	ip := c.conn.RemoteAddr().(*net.TCPAddr).IP.String()
	log.Printf("Banning IP %v for 24 hours", ip)
	s.banned.PushBack(&BannedIP{ip, time.Now().Add(24 * time.Hour)})
}

func (s Server) IsBannedClient(c *Client) bool {
	ip := c.conn.RemoteAddr().(*net.TCPAddr).IP.String()
	now := time.Now()
	for e := s.banned.Front(); e != nil; {
		entry := e.Value.(*BannedIP)
		e2 := e
		e = e.Next()
		if entry.until.Before(now) {
			log.Printf("IP %v is no longer banned", entry.ip)
			s.banned.Remove(e2)
		} else if entry.ip == ip {
			return true
		}
	}
	return false
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
	s.BroadcastToIrcFromUser(message, "")
}

func (s Server) BroadcastToIrcFromUser(message, nick string) {
	select {
	case s.irc.messagesToIRC <- Message{
		message: message,
		nick:    nick,
	}:
	default:
		log.Println("Message queue to IRC full")
	}
}

func RunServer(db UserDb, irc *IRCBridgerChannels, hostname string) {
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

	server := CreateServerUsing(C, db, irc, hostname)

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

func (server *Server) RelayCreateGame(name string, password string) bool {
	if !server.relay.CreateGame(name, password) {
		log.Println("ERROR: Unable to create a game on the relay server. This should not happen")
		return false
	} else {
		return true
	}
}

func (server *Server) RelayRemoveGame(name string) bool {
	if !server.relay.RemoveGame(name) {
		log.Println("ERROR: Told to remove game %s on relay but unable to do so.")
		return false
	} else {
		return true
	}
}

// The relay informs us that the game with the given name has been connected by the host
func (server *Server) GameConnected(name string) {
	log.Printf("Relay notifies us that the host connected to its game '%s'", name)
	game := server.HasGame(name)
	if game == nil {
		log.Printf(" Game '%s' is unknown, might already been closed", name)
		return
	}
	game.SetState(*server, CONNECTABLE)
}

// The relay informs us that the game with the given name has been closed
func (server *Server) GameClosed(name string) {
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

func CreateServerUsing(acceptedConnections chan ReadWriteCloserWithIp, db UserDb, irc *IRCBridgerChannels, hostname string) *Server {
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
		relay_address:          AddressPair{"", ""},
		banned:                 list.New(),
	}
	// Get the IP addresses of our domain
	ips, err := net.LookupIP(hostname)
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
	log.Printf("Using %v and %v as IP addresses of the relay", server.relay_address.ipv4, server.relay_address.ipv6)

	server.relay = relayinterface.NewClientRPC(server)

	server.gamePingerFactory = RealGamePingerFactory{server}
	go func() {
		for {
			select {
			case m := <-irc.messagesFromIRC:
				server.BroadcastToConnectedClients("CHAT", "<IRC> "+m.nick, m.message, "public")
			case nick := <-irc.clientsJoiningIRC:
				old_client := server.HasIRCClient(nick)
				if old_client != nil {
					// Should not happen
					log.Printf("Warning: Told to add IRC client %v which is already listed", nick)
					break
				}
				client := NewIRCClient(nick)
				server.AddClient(client)
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
			case nick := <-irc.clientsLeavingIRC:
				client := server.HasIRCClient(nick)
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
	defer s.relay.CloseConnection()
	// Remove (non-IRC) clients and games that are older than this time.
	// Normally, I expect this never to remove anything, except for
	// when bugs / unexpected states make a client/game survive.
	// The timer interval should be so big that it is unrealistic for
	// clients/games to stay online for so long
	maxOnlineTime := 7 * 24 * time.Hour
	timeFormatString := "2006-01-02 15:04:05"
	cleanupTicker := time.NewTicker(maxOnlineTime)
	defer cleanupTicker.Stop()
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
		case <-cleanupTicker.C:
			removeBefore := time.Now().Add(-maxOnlineTime)
			// Games have to be checked before clients so the GAMES_UPDATE
			// message does not get lost when a client is forced to reconnect
			s.ForeachGame(func(game *Game) {
				if game.TimeLastActivity().Before(removeBefore) {
					if !game.UsesRelay() {
						log.Printf("Warning: Removing game %v, last ping at %v",
							game.Name(), game.TimeLastActivity().Format(timeFormatString))
					} else {
						log.Printf("Warning: Removing relay game %v, last change at %v",
							game.Name(), game.TimeLastActivity().Format(timeFormatString))
					}
					s.RemoveGame(game)
				}
			})
			for e := s.clients.Front(); e != nil; e = e.Next() {
				client := e.Value.(*Client)
				if client.buildId != "IRC" && client.TimeLastMessage().Before(removeBefore) {
					log.Printf("Warning: Removing client %v, last activity at %v",
						client.Name(), client.TimeLastMessage().Format(timeFormatString))
					client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
					client.Disconnect(*s)
				}
			}
		}
	}
}
