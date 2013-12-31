package main

import (
	"fmt"
	"launchpad.net/widelands-metaserver/wlms/packet"
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

	// the current connection state
	state State

	// the time when the user logged in for the first time. Relogins do not
	// update this time.
	loginTime time.Time

	// the protocol version used for communication
	protocolVersion int

	// is this a registered user/super user?
	permissions Permissions

	// name displayed in the GUI. This is guaranteed to be unique on the Server.
	userName string

	// the buildId of Widelands executable that this client is using.
	buildId string

	// The game we are currently in. nil if not in game.
	game *Game

	// Various state variables needed for fulfilling the protocol.
	startToPingTimer *time.Timer
	timeoutTimer     *time.Timer
	waitingForPong   bool
	pendingRelogin   *Client
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

func (client *Client) setGame(game *Game, server Server) {
	if client.game != game {
		client.game = game
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
	}
}

func (client *Client) Disconnect(server Server) {
	client.conn.Close()
	if client.dataStream != nil {
		close(client.dataStream)
		client.dataStream = nil
	}
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
			server.RemoveClient(client)
		})
	}()

	client.startToPingTimer.Reset(server.PingCycleTime())
	client.timeoutTimer.Reset(server.ClientSendingTimeout())
	client.waitingForPong = false

	for {
		select {
		case pkg, ok := <-client.dataStream:
			if !ok {
				client.Disconnect(*server)
				break
			}
			client.waitingForPong = false
			client.startToPingTimer.Reset(server.PingCycleTime())
			client.timeoutTimer.Reset(server.ClientSendingTimeout())

			if client.pendingRelogin != nil {
				client.pendingRelogin.SendPacket("ERROR", "RELOGIN", "CONNECTION_STILL_ALIVE")
				client.pendingRelogin.Disconnect(*server)
				client.pendingRelogin = nil
			}

			cmdName, err := pkg.ReadString()
			if err != nil {
				client.pendingRelogin.Disconnect(*server)
				break
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
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.Disconnect(*server)

		case <-client.startToPingTimer.C:
			if !client.waitingForPong {
				client.restartPingLoop(server.PingCycleTime())
			} else {
				client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
				client.Disconnect(*server)
				if client.pendingRelogin != nil {
					client.pendingRelogin.successfulRelogin(server, client)
				}
			}
		}
	}
}

func newClient(r ReadWriteCloserWithIp) *Client {
	client := &Client{
		conn:             r,
		dataStream:       make(chan *packet.Packet, 10),
		state:            HANDSHAKE,
		permissions:      UNREGISTERED,
		startToPingTimer: time.NewTimer(time.Hour * 1),
		timeoutTimer:     time.NewTimer(time.Hour * 1),
	}
	return client
}

func (client *Client) readingLoop(server Server) {
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			break
		}
		client.dataStream <- pkg
	}
	client.Disconnect(server)
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
		log.Fatalf("%s is not valid.", client.remoteIp())
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

	// Sanitize message.
	message = strings.Replace(message, "<", "&lt;", -1)

	if len(receiver) == 0 {
		server.BroadcastToConnectedClients("CHAT", client.Name(), message, "public")
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

	client.Disconnect(*server)
	server.RemoveClient(client)
	return nil
}

func (client *Client) Handle_PONG(server *Server, pkg *packet.Packet) CmdError {
	return nil
}

func (c *Client) Handle_LOGIN(server *Server, pkg *packet.Packet) CmdError {
	var isRegisteredOnServer bool
	if err := pkg.Unpack(&c.protocolVersion, &c.userName, &c.buildId, &isRegisteredOnServer); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}

	if c.protocolVersion != 0 {
		return CriticalCmdPacketError{"UNSUPPORTED_PROTOCOL"}
	}

	if isRegisteredOnServer {
		if server.HasClient(c.userName) != nil {
			return CriticalCmdPacketError{"ALREADY_LOGGED_IN"}
		}
		if !server.UserDb().ContainsName(c.userName) {
			return CriticalCmdPacketError{"WRONG_PASSWORD"}
		}
		password, err := pkg.ReadString()
		if err != nil {
			return CriticalCmdPacketError{err.Error()}
		}
		if !server.UserDb().PasswordCorrect(c.userName, password) {
			return CriticalCmdPacketError{"WRONG_PASSWORD"}
		}
		c.permissions = server.UserDb().Permissions(c.userName)
	} else {
		baseName := c.userName
		for i := 1; server.UserDb().ContainsName(c.userName) || server.HasClient(c.userName) != nil; i++ {
			c.userName = fmt.Sprintf("%s%d", baseName, i)
		}
	}

	c.loginTime = time.Now()
	log.Printf("%s logged in.", c.userName)

	c.SendPacket("LOGIN", c.userName, c.permissions.String())
	c.SendPacket("TIME", int(time.Now().Unix()))
	server.AddClient(c)
	c.setState(CONNECTED, *server)

	if len(server.Motd()) != 0 {
		c.SendPacket("CHAT", "", server.Motd(), "system")
	}
	return nil
}

func (client *Client) Handle_RELOGIN(server *Server, pkg *packet.Packet) CmdError {
	var isRegisteredOnServer bool
	var protocolVersion int
	var userName, buildId string
	if err := pkg.Unpack(&protocolVersion, &userName, &buildId, &isRegisteredOnServer); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}

	oldClient := server.HasClient(userName)
	if oldClient == nil {
		return CriticalCmdPacketError{"NOT_LOGGED_IN"}
	}

	informationMatches :=
		protocolVersion == client.protocolVersion &&
			buildId == oldClient.buildId

	if isRegisteredOnServer {
		password, err := pkg.ReadString()
		if err != nil {
			return CriticalCmdPacketError{err.Error()}
		}
		if oldClient.permissions == UNREGISTERED || !server.UserDb().PasswordCorrect(userName, password) {
			informationMatches = false
		}
	} else if oldClient.permissions != UNREGISTERED {
		informationMatches = false
	}

	if !informationMatches {
		return CriticalCmdPacketError{"WRONG_INFORMATION"}
	}

	client.loginTime = oldClient.loginTime
	client.protocolVersion = oldClient.protocolVersion
	client.permissions = oldClient.permissions
	client.userName = oldClient.userName
	client.buildId = oldClient.buildId
	client.game = oldClient.game
	if oldClient.state == RECENTLY_DISCONNECTED {
		client.successfulRelogin(server, oldClient)
	} else {
		client.state = HANDSHAKE
		oldClient.restartPingLoop(server.PingCycleTime())
		oldClient.pendingRelogin = client
	}
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

	client.setGame(NewGame(client.Name(), server, gameName, maxPlayer), *server)

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

	game.AddPlayer(client.Name())
	host := server.HasClient(game.Host())
	log.Printf("%s joined %s at IP %s.", client.userName, game.Name(), host.remoteIp())

	client.SendPacket("GAME_CONNECT", host.remoteIp())
	client.setGame(game, *server)

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
	server.BroadcastToConnectedClients("GAMES_UPDATE")

	log.Printf("%s has started.", client.game.Name())
	return nil
}

func (client *Client) Handle_GAME_DISCONNECT(server *Server, pkg *packet.Packet) CmdError {
	if client.game == nil {
		return InvalidPacketError{}
	}

	game := client.game
	client.setGame(nil, *server)

	log.Printf("%s left the game %s.", client.userName, game.Name())
	if game.Host() == client.Name() {
		log.Print("This ends the game.")
		server.RemoveGame(game)
	}
	game.RemovePlayer(client.Name())
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
		data[n+2] = game.State() == CONNECTABLE
		n += 3
	})
	client.SendPacket(data...)
	return nil
}
