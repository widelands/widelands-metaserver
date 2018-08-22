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
	IRC
)

func (p Permissions) String() string {
	switch p {
	case UNREGISTERED:
		return "UNREGISTERED"
	case REGISTERED:
		return "REGISTERED"
	case SUPERUSER:
		return "SUPERUSER"
	case IRC:
		return "IRC"

	default:
		log.Fatalf("Unknown Permissions: %d", p)
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

// The protocol versions supported by the metaserver
const (
	BUILD19 int = 0
	BUILD20 int = 5
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
	// When a network client connects with LOGIN he also sends a nonce
	// which is stored in this field. Later on it is used to recognize
	// a (re)connecting client and assign its old identity back to it.
	// For registered clients, the nonce is used as temporary storage
	// for the challenge-response processes.
	nonce string

	// The expected response to the challenge sent to a registered client.
	expectedResponse string

	// The game we are currently in. nil if not in game.
	game *Game

	// Various state variables needed for fulfilling the protocol.
	startToPingTimer *time.Timer
	timeoutTimer     *time.Timer
	waitingForPong   bool
	pendingLogin     *Client

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
		log.Fatal("Unkown state in setState")
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
	if client.conn != nil {
		_, err := client.conn.Write(packet.New(data...))
		if err != nil {
			log.Printf("Warning: Error while sending data to client %v", client.Name())
		}
	}
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

	for {
		select {
		case pkg, ok := <-client.dataStream:
			if !ok {
				if client.state != RECENTLY_DISCONNECTED {
					client.failedPong(server)
				}
				// Else the receive failed due to a Disconnect() which is fine
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
					log.Printf("Error while handling command %v for client %v: %v", cmdName, client.Name(), pkgErr.What)
					client.SendPacket("ERROR", cmdName, pkgErr.What)
				case CriticalCmdPacketError:
					log.Printf("Error while handling command %v for client %v: %v", cmdName, client.Name(), pkgErr.What)
					client.SendPacket("ERROR", cmdName, pkgErr.What)
					client.Disconnect(*server)
				case InvalidPacketError:
					log.Printf("Error while handling command %v from client %v", cmdName, client.Name())
					client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
					client.Disconnect(*server)
				default:
					log.Fatal("Unknown error type returned by handler function")
				}
			}

		case <-client.timeoutTimer.C:
			log.Printf("Timeout of client %v", client.userName)
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
	log.Printf("Client %v failed to PONG. Will disconnect", client.Name())
	client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
	client.Disconnect(*server)
	if client.pendingLogin != nil {
		if client.pendingLogin.replaceCandidates == nil {
			// legacy path
			log.Printf("Client %v has successfully relogged in", client.Name())
			client.pendingLogin.successfulRelogin(server, client)
		} else {
			log.Printf("Client %v replaced old client with that name", client.Name())
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

func NewIRCClient(nick string) *Client {
	client := &Client{
		state:       CONNECTED,
		permissions: IRC,
		userName:    nick,
		buildId:     "IRC",
		nonce:       "irc",
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
		log.Fatalf("Client %v has no valid ip address", client.userName)
	}
	return host
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

	if len(receiver) == 0 {
		server.BroadcastToConnectedClients("CHAT", client.Name(), message, "public")
		server.BroadcastToIrc(client.Name() + ": " + message)
	} else {
		recv_client := server.HasClient(receiver)
		if recv_client == nil {
			return nil
		}
		if recv_client.permissions == IRC {
			// Bad luck, whispering to IRC is not supported yet
			client.SendPacket("CHAT", "", "Private messages to IRC users are not supported.", "system")
		}
		recv_client.SendPacket("CHAT", client.Name(), message, "private")
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
	log.Printf("Client %v left. Reason: '%v'", client.Name(), reason)

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
	if c.protocolVersion != BUILD19 && c.protocolVersion != BUILD20 {
		return CriticalCmdPacketError{"UNSUPPORTED_PROTOCOL"}
	}

	if isRegisteredOnServer || c.protocolVersion >= BUILD20 {
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
		if c.protocolVersion >= BUILD20 {
			// Send a challenge for secure passwort transmission
			c.sendChallenge(server)
			return nil
		}
		failed := c.checkCredentialsLegacy(server)
		if failed != nil {
			return failed
		}
	}
	return c.findReplaceCandidates(server, isRegisteredOnServer)
}

func (c *Client) sendChallenge(server *Server) {
	// The nonce is empty when using challenge-response. Use it to store the response
	var challenge string
	var success bool
	challenge, c.expectedResponse, success = server.UserDb().GenerateChallengeResponsePairFromUsername(c.userName)
	if !success {
		// Should not happen, but who knows
		c.Disconnect(*server)
		return
	}
	c.SendPacket("PWD_CHALLENGE", challenge)
}

func (c *Client) Handle_PWD_CHALLENGE(server *Server, pkg *packet.Packet) CmdError {
	var response string
	if err := pkg.Unpack(&response); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}
	if c.expectedResponse != response {
		return CriticalCmdPacketError{"WRONG_PASSWORD"}
	}
	// Password is fine
	switch c.state {
	case HANDSHAKE:
		c.permissions = server.UserDb().Permissions(c.userName)
		c.nonce = server.UserDb().GenerateDowngradedUserNonce(c.userName, c.userName)
		return c.findReplaceCandidates(server, true)
	default:
		c.SendPacket("ERROR", "PWD_CHALLENGE", "Invalid connection state")
		c.Disconnect(*server)
	}
	return nil
}

func (c *Client) checkCredentialsLegacy(server *Server) CmdError {
	if !server.UserDb().PasswordCorrect(c.userName, c.nonce) {
		return CriticalCmdPacketError{"WRONG_PASSWORD"}
	}
	c.permissions = server.UserDb().Permissions(c.userName)
	// Everything fine
	return nil
}

func (c *Client) findReplaceCandidates(server *Server, isRegisteredOnServer bool) CmdError {
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
		c.findUnconnectedName(server)
		return nil
	}
	c.checkCandidates(server)
	return nil
}

func (c *Client) loginDone(server *Server) CmdError {

	c.loginTime = time.Now()
	log.Printf("Client %v logged in (game version %v, protocol version %v)", c.userName, c.buildId, c.protocolVersion)

	c.SendPacket("LOGIN", c.userName, c.permissions.String())
	if c.protocolVersion <= BUILD19 {
		// Old MotD, now only used in build 19 and older. Newer client display this text locally
		c.SendPacket("CHAT", "", "Welcome on the Widelands Metaserver!", "system")

	}
	c.SendPacket("TIME", int(time.Now().Unix()))
	if c.protocolVersion <= BUILD19 {
		c.SendPacket("CHAT", "", "Our forums can be found at:", "system")
		c.SendPacket("CHAT", "", "https://wl.widelands.org/forum/", "system")
		c.SendPacket("CHAT", "", "For reporting bugs, visit:", "system")
		c.SendPacket("CHAT", "", "https://wl.widelands.org/wiki/ReportingBugs/", "system")
	}
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
	if oldClient.userName != c.userName && c.permissions != UNREGISTERED {
		// TODO(Notabilis): This can probably done later when the oldClient really is replaced
		// Other username: Drop permissions
		c.nonce = server.UserDb().GenerateDowngradedUserNonce(c.userName, oldClient.userName)
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
			if c.protocolVersion >= BUILD20 && c.permissions == REGISTERED {
				c.nonce = server.UserDb().GenerateDowngradedUserNonce(baseName, c.userName)
			}
			c.permissions = UNREGISTERED
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

	if isRegisteredOnServer || protocolVersion >= BUILD20 {
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

	log.Printf("Client %v wants to reconnect.\n", client.Name())
	if oldClient.state == RECENTLY_DISCONNECTED {
		log.Printf(" Successfully immediately, because we had a recent disconnect")
		client.successfulRelogin(server, oldClient)
	} else {
		log.Printf(" Send to handshaking first")
		client.state = HANDSHAKE
		// Force a quicker ping now, so that handshaking goes smoothly.
		oldClient.restartPingLoop(server.PingCycleTime() / 3)
		oldClient.pendingLogin = client
	}
	return nil
}

func (client *Client) Handle_GAME_OPEN(server *Server, pkg *packet.Packet) CmdError {
	var gameName string
	if client.protocolVersion == BUILD19 {
		var maxPlayer int
		if err := pkg.Unpack(&gameName, &maxPlayer); err != nil {
			return CmdPacketError{err.Error()}
		}
	} else {
		if err := pkg.Unpack(&gameName); err != nil {
			return CmdPacketError{err.Error()}
		}
	}
	if server.HasGame(gameName) != nil {
		return CmdPacketError{"GAME_EXISTS"}
	}

	if client.protocolVersion == BUILD19 {
		// Client does not support the relay server. Let him host his game
		log.Printf("Starting new game '%v' on computer of host %v", gameName, client.Name())
		client.setGame(NewGame(client.userName, client.buildId, server, gameName, false /* do not use relay */), server)
	} else {
		// Client does support the relay server. Start a game there
		log.Printf("Starting new game '%v' on relay for host %v", gameName, client.Name())
		var challenge, response string
		var success bool
		if client.permissions == REGISTERED {
			challenge, response, success = server.UserDb().GenerateChallengeResponsePairFromUsername(client.userName)
		} else {
			challenge, response, success = GenerateChallengeResponsePairFromSecret(client.nonce)
		}
		if !success {
			// Should not happen
			log.Printf("Error: Failed to generate challenge/response for client %v when opening game on relay", client.userName)
			client.Disconnect(*server)
			return nil
		}
		created := server.RelayCreateGame(gameName, response)
		if !created {
			// Not good. Should not happen
			return CmdPacketError{"RELAY_ERROR"}
		}
		game := NewGame(client.userName, client.buildId, server, gameName, true /* use relay */)
		ips := server.GetRelayAddresses()
		client.SendPacket("GAME_OPEN", challenge, ips.ipv6, true, ips.ipv4)
		client.setGame(game, server)
	}

	log.Printf("Client %v hosts game '%v'", client.userName, gameName)
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

	log.Printf("Client %v joined game '%v'", client.userName, game.Name())
	if client.protocolVersion == BUILD19 {
		if game.UsesRelay() {
			// Should never happen. The game should be a legacy game,
			// since the client only sees those as open
			return CmdPacketError{"NO_SUCH_GAME"}
		}
		// Legacy client: Send the IPv4 address, which is the only one the client has
		host := server.HasClient(game.Host())
		client.SendPacket("GAME_CONNECT", host.remoteIp())
	} else {
		// Newer client which possibly supports two IPs and uses the relay
		ips := server.GetRelayAddresses()
		client.SendPacket("GAME_CONNECT", ips.ipv6, true, ips.ipv4)
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
	return nil
}

func (client *Client) Handle_GAME_DISCONNECT(server *Server, pkg *packet.Packet) CmdError {
	client.setGame(nil, server)
	return nil
}

func (client *Client) Handle_CLIENTS(server *Server, pkg *packet.Packet) CmdError {
	var nrClients int = 0
	nFields := 4
	if client.protocolVersion >= BUILD20 {
		nrClients = server.NrActiveClients()
	} else {
		// Hide IRC users in the lobby of build19 clients. They would appear
		// at the top of the player list, confusing the user
		server.ForeachActiveClient(func(otherClient *Client) {
			if otherClient.permissions != IRC {
				nrClients++
			}
		})
		nFields = 5
	}
	data := make([]interface{}, 2+nrClients*nFields)

	data[0] = "CLIENTS"
	data[1] = nrClients
	n := 2
	server.ForeachActiveClient(func(otherClient *Client) {
		if client.protocolVersion == BUILD19 && otherClient.permissions == IRC {
			return
		}
		data[n+0] = otherClient.userName
		data[n+1] = otherClient.buildId
		if otherClient.game != nil {
			data[n+2] = otherClient.game.Name()
		} else {
			data[n+2] = ""
		}
		data[n+3] = otherClient.permissions.String()
		if client.protocolVersion < 4 {
			data[n+4] = ""
		}
		n += nFields
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
		data[n+0] = game.Name()
		data[n+1] = game.BuildId()
		if client.protocolVersion == BUILD19 {
			data[n+2] = !game.UsesRelay() && game.State() == CONNECTABLE
		} else {
			if !game.UsesRelay() {
				data[n+2] = "CLOSED"
			} else if game.State() == CONNECTABLE {
				data[n+2] = "SETUP"
			} else if game.State() == RUNNING {
				data[n+2] = "RUNNING"
			} else {
				// For half a second before the host connects to the relay
				data[n+2] = "CLOSED"
			}
		}
		n += 3
	})
	client.SendPacket(data...)
	return nil
}
