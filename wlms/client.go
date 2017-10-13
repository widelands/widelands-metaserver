package main

import (
	"fmt"
	"github.com/widelands/widelands_metaserver/wlms/packet"
	"log"
	"net"
	"reflect"
	"strings"
	"time"
)

type Permissions int

const (
	UNREGISTERED Permissions = iota
	REGISTERED
	SUPERUSER
)

func (p Permissions) String() string {
	switch p {
	case UNREGISTERED:
		return "UNREGISTERED"
	case REGISTERED:
		return "REGISTERED"
	case SUPERUSER:
		return "SUPERUSER"
	default:
		log.Fatalf("Unknown Permissions: %v", p)
	}
	// Never here
	return ""
}

type State int

const (
	HANDSHAKE State = iota
	CONNECTED
	RECENTLY_DISCONNECTED
)

type Client struct {
	// The connection (net.Conn most likely) that let us talk to the other site.
	conn ReadWriteCloserWithIp

	// We always read one whole packet and send it over this to the consumer.
	dataStream chan *packet.Packet

	// the current connection state.
	state State

	// the time when the user logged in.
	loginTime time.Time

	// the protocol version used for communication.
	protocolVersion int

	// is this a registered user/super user?
	permissions Permissions

	// name displayed in the GUI. This is guaranteed to be unique on the Server.
	userName string

	// the buildId of Widelands executable that this client is using.
	buildId string

	// The nonce to link multiple connections by the same client.
	// When a network client connects with (RE)LOGIN he also sends a nonce
	// which is stored in this field. When "another" netclient connects and
	// sends TELL_IP containing the same nonce, it is considered the
	// same game client connecting with another IP.
	// This way, two connections by IPv4 and IPv6 can be matched so
	// the server learns both addresses of the client.
	// Another usage is to recognize a (re)connecting client and assign
	// its old identity back to it.
	nonce string

	// the IP of the secondary connection.
	// usually this is an IPv4 address.
	secondaryIp string

	// Whether this client has a known IPv4/6 address.
	hasV4 bool
	hasV6 bool

	// The game we are currently in. nil if not in game.
	game *Game

	// Various state variables needed for fulfilling the protocol.
	startToPingTimer *time.Timer
	timeoutTimer     *time.Timer
	waitingForPong   bool
	pendingLogin   *Client

	// A value != nil indicates that we are currently searching for a free name.
	// This is different than a relogin after a short network problem
	replaceCandidates []*Client
}

type CmdError interface{}

type CmdPacketError struct {
	What string
}
type CriticalCmdPacketError struct {
	What string
}
type InvalidPacketError struct{}

func (c Client) State() State {
	return c.state
}
func (c *Client) setState(s State, server Server) {
	need_broadcast := false
	switch s {
	case HANDSHAKE, RECENTLY_DISCONNECTED:
		need_broadcast = c.state == CONNECTED
	case CONNECTED:
		need_broadcast = c.state == HANDSHAKE || c.state == RECENTLY_DISCONNECTED
	default:
		log.Fatal("Unkown state in setState.")
	}
	c.state = s
	if need_broadcast {
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
	}
}

func (client Client) Name() string {
	return client.userName
}

func (client Client) Nonce() string {
	return client.nonce
}

func (client Client) PendingLogin() *Client {
	return client.pendingLogin
}

func (client *Client) setGame(game *Game, server *Server) {
	if client.game == game {
		return
	}

	if client.game != nil {
		game := client.game
		log.Printf("%s left the game %s.", client.userName, game.Name())
		client.game.RemovePlayer(client.Name(), server)
		client.game = nil
	}

	if game != nil {
		client.game = game
		game.AddPlayer(client.Name())
	}
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")
}

func (client *Client) Disconnect(server Server) {
	client.conn.Close()
	client.setState(RECENTLY_DISCONNECTED, server)
}

func (client *Client) SendPacket(data ...interface{}) {
	client.conn.Write(packet.New(data...))
}

func DealWithNewConnection(conn ReadWriteCloserWithIp, server *Server) {
	client := newClient(conn)
	go client.readingLoop(*server)

	defer func() {
		time.AfterFunc(server.ClientForgetTimeout(), func() {
			if server.HasClient(client.Name()) == client {
				client.setGame(nil, server)
				server.RemoveClient(client)
			}
		})
	}()

	client.startToPingTimer.Reset(server.PingCycleTime())
	client.timeoutTimer.Reset(server.ClientSendingTimeout())
	client.waitingForPong = false

	ip := net.ParseIP(client.remoteIp())
	if ip.To4() != nil {
		client.hasV4 = true
	} else {
		client.hasV6 = true
	}

	for {
		select {
		case pkg, ok := <-client.dataStream:
			if !ok {
				if client.state != RECENTLY_DISCONNECTED {
					// The receival failed due to a disconnect
					client.failedPong(server)
				}
				return
			}
			client.waitingForPong = false
			client.startToPingTimer.Reset(server.PingCycleTime())
			client.timeoutTimer.Reset(server.ClientSendingTimeout())

			if client.pendingLogin != nil {
				if client.pendingLogin.replaceCandidates == nil {
					// legacy path
					client.pendingLogin.SendPacket("ERROR", "RELOGIN", "CONNECTION_STILL_ALIVE")
					client.pendingLogin.Disconnect(*server)
				} else {
					client.pendingLogin.checkCandidates(server)
				}
				client.pendingLogin = nil
			}

			cmdName, err := pkg.ReadString()
			if err != nil {
				client.Disconnect(*server)
				return
			}

			handlerFunc := reflect.ValueOf(client).MethodByName(strings.Join([]string{"Handle_", cmdName}, ""))
			pkgErr := CmdError(InvalidPacketError{})
			if handlerFunc.IsValid() {
				handlerFunc := handlerFunc.Interface().(func(*Server, *packet.Packet) CmdError)
				pkgErr = handlerFunc(server, pkg)
			}
			if pkgErr != nil {
				switch pkgErr := pkgErr.(type) {
				case CmdPacketError:
					client.SendPacket("ERROR", cmdName, pkgErr.What)
				case CriticalCmdPacketError:
					client.SendPacket("ERROR", cmdName, pkgErr.What)
					client.Disconnect(*server)
				case InvalidPacketError:
					client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
					client.Disconnect(*server)
				default:
					log.Fatal("Unknown error type returned by handler function.")
				}
			}

		case <-client.timeoutTimer.C:
			log.Printf("Timeout of %s", client.userName)
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.Disconnect(*server)

		case <-client.startToPingTimer.C:
			if !client.waitingForPong {
				client.restartPingLoop(server.PingCycleTime())
			} else {
				client.failedPong(server)
			}
		}
	}
}

func (client *Client) failedPong(server *Server) {
	log.Printf("%s failed to PONG. Will disconnect.", client.Name())
	client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
	client.Disconnect(*server)
	if client.pendingLogin != nil {
		if client.pendingLogin.replaceCandidates == nil {
			// legacy path
			log.Printf("%s has successfully relogged in.", client.Name())
			client.pendingLogin.successfulRelogin(server, client)
		} else {
			log.Printf("%s replaced old client with that name.", client.Name())
			pending := client.pendingLogin
			pending.userName = client.userName
			server.RemoveClient(client)
			pending.loginDone(server)
		}
	}

}

func newClient(r ReadWriteCloserWithIp) *Client {
	client := &Client{
		conn:              r,
		dataStream:        make(chan *packet.Packet, 10),
		state:             HANDSHAKE,
		permissions:       UNREGISTERED,
		startToPingTimer:  time.NewTimer(time.Hour * 1),
		timeoutTimer:      time.NewTimer(time.Hour * 1),
		replaceCandidates: nil,
	}
	return client
}

func (client *Client) readingLoop(server Server) {
	defer close(client.dataStream)
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			return
		}
		client.dataStream <- pkg
	}
}

func (client *Client) restartPingLoop(pingCycleTime time.Duration) {
	if client.state == CONNECTED {
		client.SendPacket("PING")
		client.waitingForPong = true
	}
	client.startToPingTimer.Reset(pingCycleTime)
}

func (client Client) remoteIp() string {
	host, _, err := net.SplitHostPort(client.conn.RemoteAddr().String())
	if err != nil {
		log.Fatalf("%s has no valid ip address.", client.userName)
	}
	return host
}

func (client Client) otherIp() string {
	return client.secondaryIp
}

func (newClient *Client) successfulRelogin(server *Server, oldClient *Client) {
	server.RemoveClient(oldClient)

	newClient.SendPacket("RELOGIN")
	server.AddClient(newClient)
	newClient.setState(CONNECTED, *server)
}

func (client *Client) Handle_CHAT(server *Server, pkg *packet.Packet) CmdError {
	var message, receiver string
	if err := pkg.Unpack(&message, &receiver); err != nil {
		return CmdPacketError{err.Error()}
	}

	// Sanitize message.
	message = strings.Replace(message, "<", "&lt;", -1)

	if len(receiver) == 0 {
		server.BroadcastToConnectedClients("CHAT", client.Name(), message, "public")
		server.BroadcastToIrc(client.Name() + ": " + message)
	} else {
		recv_client := server.HasClient(receiver)
		if recv_client != nil {
			recv_client.SendPacket("CHAT", client.Name(), message, "private")
		}
	}
	return nil
}

func (client *Client) Handle_MOTD(server *Server, pkg *packet.Packet) CmdError {
	var message string
	if err := pkg.Unpack(&message); err != nil {
		return CmdPacketError{err.Error()}
	}

	if client.permissions != SUPERUSER {
		return CmdPacketError{"DEFICIENT_PERMISSION"}
	}
	server.SetMotd(message)
	server.BroadcastToConnectedClients("CHAT", "", server.Motd(), "system")
	return nil
}

func (client *Client) Handle_ANNOUNCEMENT(server *Server, pkg *packet.Packet) CmdError {
	var message string
	if err := pkg.Unpack(&message); err != nil {
		return CmdPacketError{err.Error()}
	}

	if client.permissions != SUPERUSER {
		return CmdPacketError{"DEFICIENT_PERMISSION"}
	}
	server.BroadcastToConnectedClients("CHAT", "", message, "system")
	return nil
}

func (client *Client) Handle_DISCONNECT(server *Server, pkg *packet.Packet) CmdError {
	var reason string
	if err := pkg.Unpack(&reason); err != nil {
		return CmdPacketError{err.Error()}
	}
	log.Printf("%s left. Reason: '%s'", client.Name(), reason)

	client.setGame(nil, server)

	client.Disconnect(*server)
	server.RemoveClient(client)
	return nil
}

func (client *Client) Handle_PONG(server *Server, pkg *packet.Packet) CmdError {
	// Nothing to do, is handled in the main receive loop
	return nil
}

func (c *Client) Handle_LOGIN(server *Server, pkg *packet.Packet) CmdError {
	var isRegisteredOnServer bool
	if err := pkg.Unpack(&c.protocolVersion, &c.userName, &c.buildId, &isRegisteredOnServer); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}

	// Check protocol version
	if c.protocolVersion != 0 && c.protocolVersion != 2 {
		return CriticalCmdPacketError{"UNSUPPORTED_PROTOCOL"}
	}

	if isRegisteredOnServer || c.protocolVersion == 2 {
		nonce, err := pkg.ReadString()
		if err != nil {
			return CriticalCmdPacketError{err.Error()}
		}
		c.nonce = nonce
	}

	// Check if registered. If it is, check credentials. If invalid, abort.
	if isRegisteredOnServer {
		if !server.UserDb().ContainsName(c.userName) {
			return CriticalCmdPacketError{"WRONG_PASSWORD"}
		}
		if !server.UserDb().PasswordCorrect(c.userName, c.nonce) {
			return CriticalCmdPacketError{"WRONG_PASSWORD"}
		}
		c.permissions = server.UserDb().Permissions(c.userName)
	}
	// Check for clients which are using the same nonce
	c.replaceCandidates = server.FindClientsToReplace(c.nonce, c.userName)
	if len(c.replaceCandidates) == 0 {
		// Noone connected with our nonce
		// TODO(Notabilis): Maybe do the ContainsName() check here case-insensitive?
		//                  Having "Peter" and "peter" is quite strange. Same for HasClient().
		if server.HasClient(c.userName) == nil && (isRegisteredOnServer || !server.UserDb().ContainsName(c.userName)) {
			// Name not in use
			return c.loginDone(server)
		}
		// Name is in use or registered for someone else: Search for a free one
		c.permissions = UNREGISTERED
		c.findUnconnectedName(server)
		return nil
	}
	c.checkCandidates(server)
	return nil;
}

func (c *Client) loginDone(server *Server) CmdError {

	c.loginTime = time.Now()
	log.Printf("%s logged in.", c.userName)

	c.SendPacket("LOGIN", c.userName, c.permissions.String())
	c.SendPacket("TIME", int(time.Now().Unix()))
	server.AddClient(c)
	c.setState(CONNECTED, *server)

	if len(server.Motd()) != 0 {
		c.SendPacket("CHAT", "", server.Motd(), "system")
	}
	c.replaceCandidates = nil
	return nil

}

func (c *Client) checkCandidates(server *Server) {
	if len(c.replaceCandidates) == 0 {
		c.findUnconnectedName(server)
		return
	}
	var oldClient *Client
	oldClient, c.replaceCandidates = c.replaceCandidates[0], c.replaceCandidates[1:]
	if oldClient.userName != c.userName {
		// Other username: Drop permissions
		c.permissions = UNREGISTERED
	}
	if oldClient.pendingLogin != nil {
		// If there is a login pending, skip the old client
		c.checkCandidates(server)
		return
	}
	if oldClient.state == RECENTLY_DISCONNECTED {
		// Already known as offline
		c.userName = oldClient.userName
		server.RemoveClient(oldClient)
		c.loginDone(server)
	} else {
		// Start a fast ping and check whether the client is still active
		oldClient.restartPingLoop(server.PingCycleTime() / 5)
		oldClient.pendingLogin = c
	}
}

func (c *Client) findUnconnectedName(server *Server) {
	c.permissions = UNREGISTERED
	nameIndex := 0
	baseName := c.userName
	for {
		// Generate new name
		nameIndex++
		c.userName = fmt.Sprintf("%s%d", baseName, nameIndex)

		if server.UserDb().ContainsName(c.userName) {
			continue
		}

		oldClient := server.HasClient(c.userName)
		if oldClient == nil {
			// Found a free name
			c.loginDone(server)
			return
		}
	}
}

// Only for legacy clients
func (client *Client) Handle_RELOGIN(server *Server, pkg *packet.Packet) CmdError {
	var isRegisteredOnServer bool
	var protocolVersion int
	var userName, buildId, nonce string
	if err := pkg.Unpack(&protocolVersion, &userName, &buildId, &isRegisteredOnServer); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}

	if isRegisteredOnServer || protocolVersion == 2 {
		n, err := pkg.ReadString()
		if err != nil {
			return CriticalCmdPacketError{err.Error()}
		}
		nonce = n
	}

	oldClient := server.HasClient(userName)
	if oldClient == nil {
		return CriticalCmdPacketError{"NOT_LOGGED_IN"}
	}

	informationMatches :=
		protocolVersion == oldClient.protocolVersion &&
			buildId == oldClient.buildId &&
			nonce == oldClient.nonce

	// Don't check permissions since they might have been different from what the client requested.
	// The current client understands the "permission downgrade" but I can't modify the old one.

	if !informationMatches {
		return CriticalCmdPacketError{"WRONG_INFORMATION"}
	}

	client.loginTime = oldClient.loginTime
	client.protocolVersion = oldClient.protocolVersion
	client.permissions = oldClient.permissions
	client.userName = oldClient.userName
	client.buildId = oldClient.buildId
	client.game = oldClient.game
	client.nonce = oldClient.nonce

	log.Printf("%s wants to reconnect.\n", client.Name())
	if oldClient.state == RECENTLY_DISCONNECTED {
		log.Printf("Successfully immediately, because we had a recent disconnect.")
		client.successfulRelogin(server, oldClient)
	} else {
		log.Printf("Send to handshaking first.")
		client.state = HANDSHAKE
		// Force a quicker ping now, so that handshaking goes smoothly.
		oldClient.restartPingLoop(server.PingCycleTime() / 3)
		oldClient.pendingLogin = client
	}
	return nil
}

func (client *Client) Handle_TELL_IP(server *Server, pkg *packet.Packet) CmdError {
	var protocolVersion int
	var name, nonce string
	if err := pkg.Unpack(&protocolVersion, &name, &nonce); err != nil {
		return CmdPacketError{err.Error()}
	}

	if protocolVersion != 2 {
		return CriticalCmdPacketError{"UNSUPPORTED_PROTOCOL"}
	}

	old_client := server.HasClient(name)
	if old_client == nil || old_client.userName != name || old_client.nonce != nonce {
		log.Printf("Someone failed to register an IP for %s.", old_client.Name())
		return CriticalCmdPacketError{"NOT_LOGGED_IN"}
	}

	// We found the existing connection of this client.
	// Update his IP and close this connection.
	old_client.secondaryIp = client.remoteIp()
	ip := net.ParseIP(old_client.otherIp())
	if ip.To4() != nil {
		old_client.hasV4 = true
	} else {
		old_client.hasV6 = true
	}
	log.Printf("%s is now known to use %s and %s.", old_client.Name(), old_client.remoteIp(), old_client.otherIp())
	client.Disconnect(*server)
	// Tell the client to get a new list of games. The availability of games might have changed now that
	// he supports more IP versions
	old_client.SendPacket("GAMES_UPDATE")

	return nil
}


func (client *Client) Handle_GAME_OPEN(server *Server, pkg *packet.Packet) CmdError {
	var gameName string
	var maxPlayer int
	if err := pkg.Unpack(&gameName, &maxPlayer); err != nil {
		return CmdPacketError{err.Error()}
	}
	if server.HasGame(gameName) != nil {
		return CmdPacketError{"GAME_EXISTS"}
	}

	client.setGame(NewGame(client.userName, server, gameName, maxPlayer), server)

	log.Printf("%s hosts %s.", client.userName, gameName)
	return nil
}

func (client *Client) Handle_GAME_CONNECT(server *Server, pkg *packet.Packet) CmdError {
	gameName, err := pkg.ReadString()
	if err != nil {
		return CmdPacketError{err.Error()}
	}

	game := server.HasGame(gameName)
	if game == nil {
		return CmdPacketError{"NO_SUCH_GAME"}
	}
	if game.NrPlayers() == game.MaxPlayers() {
		return CmdPacketError{"GAME_FULL"}
	}

	host := server.HasClient(game.Host())
	log.Printf("%s joined %s.", client.userName, game.Name())

	var ipv4, ipv6 string
	ip := net.ParseIP(host.remoteIp())
	if ip.To4() != nil {
		ipv4 = host.remoteIp()
		ipv6 = host.otherIp()
	} else {
		ipv4 = host.otherIp()
		ipv6 = host.remoteIp()
	}
	if client.protocolVersion == 0 {
		// Legacy client: Send the IPv4 address
		client.SendPacket("GAME_CONNECT", ipv4)
		// One of the two has to be IPv4, otherwise the client wouldn't come this
		// far anyway (game would appear closed)
	} else {
		// Newer client which supports two IPs
		// Only send him the IPs he can deal with
		if client.hasV4 && client.hasV6 && game.State() == CONNECTABLE_BOTH {
			// Both client and server have both IPs
			client.SendPacket("GAME_CONNECT", ipv6, true, ipv4)
		} else if client.hasV4 && game.State() == CONNECTABLE_V4 {
			// Client and server have an IPv4 address
			client.SendPacket("GAME_CONNECT", ipv4, false)
		} else if client.hasV6 && game.State() == CONNECTABLE_V6 {
			// Client and server have an IPv6 address
			client.SendPacket("GAME_CONNECT", ipv6, false)
		}
	}
	client.setGame(game, server)

	return nil
}

func (client *Client) Handle_GAME_START(server *Server, pkg *packet.Packet) CmdError {
	if client.game == nil {
		return InvalidPacketError{}
	}

	if client.game.Host() != client.Name() {
		return CmdPacketError{"DEFICIENT_PERMISSION"}
	}

	client.SendPacket("GAME_START")
	client.game.SetState(*server, RUNNING)

	log.Printf("%s has started.", client.game.Name())
	return nil
}

func (client *Client) Handle_GAME_DISCONNECT(server *Server, pkg *packet.Packet) CmdError {
	client.setGame(nil, server)
	return nil
}

func (client *Client) Handle_CLIENTS(server *Server, pkg *packet.Packet) CmdError {
	nrClients := server.NrActiveClients()
	data := make([]interface{}, 2+nrClients*5)

	data[0] = "CLIENTS"
	data[1] = nrClients
	n := 2
	server.ForeachActiveClient(func(otherClient *Client) {
		data[n+0] = otherClient.userName
		data[n+1] = otherClient.buildId
		if otherClient.game != nil {
			data[n+2] = otherClient.game.Name()
		} else {
			data[n+2] = ""
		}
		data[n+3] = otherClient.permissions.String()
		data[n+4] = ""
		n += 5
	})
	client.SendPacket(data...)
	return nil
}

func (client *Client) Handle_GAMES(server *Server, pkg *packet.Packet) CmdError {
	nrGames := server.NrGames()
	data := make([]interface{}, 2+nrGames*3)

	data[0] = "GAMES"
	data[1] = nrGames
	n := 2
	server.ForeachGame(func(game *Game) {
		host := server.HasClient(game.Host())
		data[n+0] = game.Name()
		data[n+1] = host.buildId
		// A game is connectable when the client supports the IP version of the game
		// (and the game is connectable itself, of course)
		connectable := game.State() == CONNECTABLE_BOTH
		if client.hasV4 && game.State() == CONNECTABLE_V4 {
			connectable = true
		} else if client.hasV6 && game.State() == CONNECTABLE_V6 {
			connectable = true
		}
		data[n+2] = connectable
		n += 3
	})
	client.SendPacket(data...)
	return nil
}
