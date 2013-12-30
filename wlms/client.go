package main

import (
	"fmt"
	"launchpad.net/wlmetaserver/wlms/packet"
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

func (c Client) State() State {
	return c.state
}

func (client Client) Name() string {
	return client.userName
}

func (client *Client) Disconnect() {
	client.conn.Close()
	if client.dataStream != nil {
		close(client.dataStream)
		client.dataStream = nil
	}
	client.state = RECENTLY_DISCONNECTED
}

func (client *Client) SendPacket(data ...interface{}) {
	client.conn.Write(packet.New(data...))
}

func DealWithNewConnection(conn ReadWriteCloserWithIp, server *Server) {
	client := newClient(conn)
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
				client.Disconnect()
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				break
			}
			client.waitingForPong = false
			client.startToPingTimer.Reset(server.PingCycleTime())
			client.timeoutTimer.Reset(server.ClientSendingTimeout())

			if client.pendingRelogin != nil {
				client.pendingRelogin.SendPacket("ERROR", "RELOGIN", "CONNECTION_STILL_ALIVE")
				client.pendingRelogin.Disconnect()
				client.pendingRelogin = nil
			}

			cmdName, err := pkg.ReadString()
			if err != nil {
				client.Disconnect()
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				break
			}

			handlerFunc := reflect.ValueOf(client).MethodByName(strings.Join([]string{"Handle_", cmdName}, ""))
			if handlerFunc.IsValid() {
				handlerFunc := handlerFunc.Interface().(func(*Server, *packet.Packet) (string, bool))
				errString, done := handlerFunc(server, pkg)
				if errString != "" {
					client.SendPacket("ERROR", cmdName, errString)
				}
				// NOCOM(#sirver): kill done
				if done {
					client.Disconnect()
				}
			} else {
				client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
				client.Disconnect()
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
			}
		case <-client.timeoutTimer.C:
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.Disconnect()
			server.BroadcastToConnectedClients("CLIENTS_UPDATE")
		case <-client.startToPingTimer.C:
			if !client.waitingForPong {
				client.restartPingLoop(server.PingCycleTime())
			} else {
				client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
				client.Disconnect()
				if client.pendingRelogin != nil {
					client.pendingRelogin.successfulRelogin(server, client)
				} else {
					server.BroadcastToConnectedClients("CLIENTS_UPDATE")
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
	go client.readingLoop()
	return client
}

func (client *Client) readingLoop() {
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			break
		}
		client.dataStream <- pkg
	}
	client.Disconnect()
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
	newClient.SendPacket("RELOGIN")
	// Replace the client.
	newClient.state = CONNECTED
	server.AddClient(newClient)
	oldClient.Disconnect()
	server.RemoveClient(oldClient)
}

func (client *Client) Handle_CHAT(server *Server, pkg *packet.Packet) (string, bool) {
	var message, receiver string
	if err := pkg.Unpack(&message, &receiver); err != nil {
		return err.Error(), false
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
	return "", false
}

func (client *Client) Handle_MOTD(server *Server, pkg *packet.Packet) (string, bool) {
	var message string
	if err := pkg.Unpack(&message); err != nil {
		return err.Error(), false
	}

	if client.permissions != SUPERUSER {
		return "DEFICIENT_PERMISSION", false
	}
	server.SetMotd(message)
	server.BroadcastToConnectedClients("CHAT", "", server.Motd(), "system")

	return "", false
}

func (client *Client) Handle_ANNOUNCEMENT(server *Server, pkg *packet.Packet) (string, bool) {
	var message string
	if err := pkg.Unpack(&message); err != nil {
		return err.Error(), false
	}

	if client.permissions != SUPERUSER {
		return "DEFICIENT_PERMISSION", false
	}
	server.BroadcastToConnectedClients("CHAT", "", message, "system")

	return "", false
}

func (client *Client) Handle_DISCONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	var reason string
	if err := pkg.Unpack(&reason); err != nil {
		return err.Error(), false
	}
	log.Printf("%s left. Reason: '%s'", client.Name(), reason)

	server.RemoveClient(client)

	return "", true
}

func (client *Client) Handle_PONG(server *Server, pkg *packet.Packet) (string, bool) {
	return "", false
}

func (c *Client) Handle_LOGIN(server *Server, pkg *packet.Packet) (string, bool) {
	var isRegisteredOnServer bool
	if err := pkg.Unpack(&c.protocolVersion, &c.userName, &c.buildId, &isRegisteredOnServer); err != nil {
		return err.Error(), true
	}

	if c.protocolVersion != 0 {
		return "UNSUPPORTED_PROTOCOL", true
	}

	if isRegisteredOnServer {
		if server.HasClient(c.userName) != nil {
			return "ALREADY_LOGGED_IN", true
		}
		if !server.UserDb().ContainsName(c.userName) {
			return "WRONG_PASSWORD", true
		}
		password, err := pkg.ReadString()
		if err != nil {
			return err.Error(), true
		}
		if !server.UserDb().PasswordCorrect(c.userName, password) {
			return "WRONG_PASSWORD", true
		}
		c.permissions = server.UserDb().Permissions(c.userName)
	} else {
		baseName := c.userName
		for i := 1; server.UserDb().ContainsName(c.userName) || server.HasClient(c.userName) != nil; i++ {
			c.userName = fmt.Sprintf("%s%d", baseName, i)
		}
	}

	c.loginTime = time.Now()
	c.state = CONNECTED
	log.Printf("%s logged in.", c.userName)

	c.SendPacket("LOGIN", c.userName, c.permissions.String())
	c.SendPacket("TIME", int(time.Now().Unix()))
	server.AddClient(c)
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	if len(server.Motd()) != 0 {
		c.SendPacket("CHAT", "", server.Motd(), "system")
	}
	return "", false
}

func (client *Client) Handle_RELOGIN(server *Server, pkg *packet.Packet) (string, bool) {
	var isRegisteredOnServer bool
	var protocolVersion int
	var userName, buildId string
	if err := pkg.Unpack(&protocolVersion, &userName, &buildId, &isRegisteredOnServer); err != nil {
		return err.Error(), true
	}

	oldClient := server.HasClient(userName)
	if oldClient == nil {
		return "NOT_LOGGED_IN", true
	}

	informationMatches :=
		protocolVersion == client.protocolVersion &&
			buildId == oldClient.buildId

	if isRegisteredOnServer {
		password, err := pkg.ReadString()
		if err != nil {
			return err.Error(), true
		}
		if oldClient.permissions == UNREGISTERED || !server.UserDb().PasswordCorrect(userName, password) {
			informationMatches = false
		}
	} else if oldClient.permissions != UNREGISTERED {
		informationMatches = false
	}

	if !informationMatches {
		return "WRONG_INFORMATION", true
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
	return "", false
}

func (client *Client) Handle_GAME_OPEN(server *Server, pkg *packet.Packet) (string, bool) {
	var gameName string
	var maxPlayer int
	if err := pkg.Unpack(&gameName, &maxPlayer); err != nil {
		return err.Error(), false
	}
	if server.HasGame(gameName) != nil {
		return "GAME_EXISTS", false
	}
	client.game = NewGame(client.Name(), server, gameName, maxPlayer)
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	log.Printf("%s hosts %s.", client.userName, gameName)

	return "", false
}

func (client *Client) Handle_GAME_CONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	gameName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	game := server.HasGame(gameName)
	if game == nil {
		return "NO_SUCH_GAME", false
	}
	if game.NrPlayers() == game.MaxPlayers() {
		return "GAME_FULL", false
	}

	game.AddPlayer(client.Name())
	host := server.HasClient(game.Host())
	log.Printf("%s joined %s at IP %s.", client.userName, game.Name(), host.remoteIp())
	client.SendPacket("GAME_CONNECT", host.remoteIp())

	client.game = game
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	return "", false
}

func (client *Client) Handle_GAME_START(server *Server, pkg *packet.Packet) (string, bool) {
	if client.game == nil {
		client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
		// NOCOM(#sirver): disconnect could call set state?
		client.Disconnect()
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
		return "", true
	}

	if client.game.Host() != client.Name() {
		return "DEFICIENT_PERMISSION", false
	}

	client.SendPacket("GAME_START")
	server.BroadcastToConnectedClients("GAMES_UPDATE")
	log.Printf("%s has started.", client.game.Name())

	return "", false
}

func (client *Client) Handle_GAME_DISCONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	if client.game == nil {
		client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
		client.Disconnect()
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
		return "", true
	}

	// NOCOM(#sirver): Add a SetGame(server)
	game := client.game
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	client.game = nil

	log.Printf("%s left the game %s.", client.userName, game.Name())
	if game.Host() == client.Name() {
		log.Print("This ends the game.")
		server.RemoveGame(game)
	}
	game.RemovePlayer(client.Name())

	return "", false
}

func (client *Client) Handle_CLIENTS(server *Server, pkg *packet.Packet) (string, bool) {
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

	return "", false
}

func (client *Client) Handle_GAMES(server *Server, pkg *packet.Packet) (string, bool) {
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

	return "", false
}
