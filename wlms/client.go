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

	// the time when the user logged in for the first time. Relogins do not
	// update this time.
	loginTime time.Time

	// the protocol version used for communication
	protocolVersion int

	// the current connection state
	state State

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

func (c Client) State() State {
	return c.state
}

func (client *Client) Disconnect() error {
	log.Printf("In Disconnect\n")
	client.conn.Close()
	if client.dataStream != nil {
		close(client.dataStream)
		client.dataStream = nil
	}
	return nil
}

func (client *Client) SendPacket(data ...interface{}) {
	log.Printf("Sending to %s: %v\n", client.userName, data)
	client.conn.Write(packet.New(data...))
}

func (client Client) Name() string {
	return client.userName
}

func (client *Client) SetGame(game *Game) {
	client.game = game
}

func (client Client) remoteIp() string {
	host, _, err := net.SplitHostPort(client.conn.RemoteAddr().String())
	if err != nil {
		log.Fatalf("%s is not valid.", client.remoteIp())
	}
	return host
}

func DealWithNewConnection(conn ReadWriteCloserWithIp, server *Server) {
	client := newClient(conn)
	log.Print("Starting Goroutine: DealWithNewConnection")
	client.startToPingTimer.Reset(server.PingCycleTime())
	client.timeoutTimer.Reset(server.ClientSendingTimeout())
	client.waitingForPong = false

	for done := false; !done; {
		select {
		case pkg, ok := <-client.dataStream:
			if !ok {
				client.state = RECENTLY_DISCONNECTED
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				client.Disconnect()
				done = true
				break
			}
			client.waitingForPong = false
			if client.pendingRelogin != nil {
				client.pendingRelogin.SendPacket("ERROR", "RELOGIN", "CONNECTION_STILL_ALIVE")
				client.pendingRelogin.Disconnect()
				client.pendingRelogin = nil
			}
			client.startToPingTimer.Reset(server.PingCycleTime())
			client.timeoutTimer.Reset(server.ClientSendingTimeout())

			cmdName, err := pkg.ReadString()
			if err != nil {
				done = true
				break
			}

			handlerFunc := reflect.ValueOf(client).MethodByName(strings.Join([]string{"Handle", cmdName}, ""))
			if handlerFunc.IsValid() {
				handlerFunc := handlerFunc.Interface().(func(*Server, *packet.Packet) (string, bool))
				errString := ""
				errString, done = handlerFunc(server, pkg)
				if errString != "" {
					client.SendPacket("ERROR", cmdName, errString)
				}
			} else {
				log.Printf("%s: Garbage packet %s", client.Name(), cmdName)
				client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
				client.Disconnect()
				server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				done = true
			}
		case <-client.timeoutTimer.C:
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.state = RECENTLY_DISCONNECTED
			client.Disconnect()
			server.BroadcastToConnectedClients("CLIENTS_UPDATE")
			done = true
		case <-client.startToPingTimer.C:
			if client.waitingForPong {
				client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
				client.state = RECENTLY_DISCONNECTED
				client.Disconnect()
				if client.pendingRelogin != nil {
					client.pendingRelogin.SendPacket("RELOGIN")
					log.Printf("client.pendingRelogin.state.String(): %v\n", client.pendingRelogin.state)
					client.pendingRelogin.state = CONNECTED
					// Replace the client.
					server.AddClient(client.pendingRelogin)
					server.RemoveClient(client)
				} else {
					server.BroadcastToConnectedClients("CLIENTS_UPDATE")
				}
				done = true
				break
			}
			client.restartPingLoop(server.PingCycleTime())
		}
	}
	client.Disconnect()

	time.AfterFunc(server.ClientForgetTimeout(), func() {
		server.RemoveClient(client)
	})

	log.Print("Ending Goroutine: DealWithNewConnection")
}

func (client *Client) readingLoop() {
	log.Print("Starting Goroutine: readingLoop")
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			log.Printf("Error reading packet: %v\n", err)
			break
		}
		client.dataStream <- pkg
	}
	client.Disconnect()
	log.Print("Ending Goroutine: readingLoop")
}

func (client *Client) restartPingLoop(pingCycleTime time.Duration) {
	if client.state == CONNECTED {
		client.SendPacket("PING")
		client.waitingForPong = true
	}
	client.startToPingTimer.Reset(pingCycleTime)
}

func (client *Client) HandleCHAT(server *Server, pkg *packet.Packet) (string, bool) {
	message, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	// Sanitize message.
	message = strings.Replace(message, "<", "&lt;", -1)
	receiver, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

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

func (client *Client) HandleMOTD(server *Server, pkg *packet.Packet) (string, bool) {
	message, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	if client.permissions != SUPERUSER {
		return "DEFICIENT_PERMISSION", false
	}
	server.SetMotd(message)
	server.BroadcastToConnectedClients("CHAT", "", server.Motd(), "system")

	return "", false
}

func (client *Client) HandleANNOUNCEMENT(server *Server, pkg *packet.Packet) (string, bool) {
	message, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	if client.permissions != SUPERUSER {
		return "DEFICIENT_PERMISSION", false
	}
	server.BroadcastToConnectedClients("CHAT", "", message, "system")

	return "", false
}

func (client *Client) HandleDISCONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	reason, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}
	log.Printf("%s: leaving. Reason: '%s'", client.Name(), reason)

	server.RemoveClient(client)

	return "", true
}

func (client *Client) HandlePONG(server *Server, pkg *packet.Packet) (string, bool) {
	return "", false
}

func (client *Client) HandleLOGIN(server *Server, pkg *packet.Packet) (string, bool) {
	protocolVersion, err := pkg.ReadInt()
	if err != nil {
		return err.Error(), true
	}
	if protocolVersion != 0 {
		return "UNSUPPORTED_PROTOCOL", true
	}

	userName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}

	buildId, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}

	isRegisteredOnServer, err := pkg.ReadBool()
	if err != nil {
		return err.Error(), true
	}

	if isRegisteredOnServer {
		if server.HasClient(userName) != nil {
			return "ALREADY_LOGGED_IN", true
		}
		if !server.UserDb().ContainsName(userName) {
			return "WRONG_PASSWORD", true
		}
		password, err := pkg.ReadString()
		if err != nil {
			return err.Error(), true
		}
		if !server.UserDb().PasswordCorrect(userName, password) {
			return "WRONG_PASSWORD", true
		}
		client.permissions = server.UserDb().Permissions(userName)
	} else {
		baseName := userName
		for i := 1; server.UserDb().ContainsName(userName) || server.HasClient(userName) != nil; i++ {
			userName = fmt.Sprintf("%s%d", baseName, i)
		}
	}

	client.protocolVersion = protocolVersion
	client.buildId = buildId
	client.userName = userName
	client.loginTime = time.Now()
	client.state = CONNECTED

	client.SendPacket("LOGIN", userName, client.permissions.String())
	client.SendPacket("TIME", int(time.Now().Unix()))
	server.AddClient(client)
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	if len(server.Motd()) != 0 {
		client.SendPacket("CHAT", "", server.Motd(), "system")
	}

	return "", false
}

func (client *Client) HandleRELOGIN(server *Server, pkg *packet.Packet) (string, bool) {
	protocolVersion, err := pkg.ReadInt()
	if err != nil {
		return err.Error(), true
	}

	userName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}

	oldClient := server.HasClient(userName)
	if oldClient == nil {
		return "NOT_LOGGED_IN", true
	}
	informationMatches := true

	if protocolVersion != client.protocolVersion {
		informationMatches = false
	}

	buildId, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}
	if buildId != oldClient.buildId {
		informationMatches = false
	}

	isRegisteredOnServer, err := pkg.ReadBool()
	if err != nil {
		return err.Error(), true
	}

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

	client.protocolVersion = oldClient.protocolVersion
	client.buildId = oldClient.buildId
	client.userName = oldClient.userName
	client.loginTime = oldClient.loginTime
	client.game = oldClient.game
	if oldClient.state == RECENTLY_DISCONNECTED {
		client.SendPacket("RELOGIN")
		client.state = CONNECTED
		server.AddClient(client)
		server.RemoveClient(oldClient)
	} else {
		client.state = HANDSHAKE
		oldClient.restartPingLoop(server.PingCycleTime())
		oldClient.pendingRelogin = client
	}

	return "", false
}

func (client *Client) HandleGAME_OPEN(server *Server, pkg *packet.Packet) (string, bool) {
	gameName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	if server.HasGame(gameName) != nil {
		return "GAME_EXISTS", false
	}

	maxPlayer, err := pkg.ReadInt()
	if err != nil {
		return err.Error(), false
	}
	game := NewGame(client, server, gameName, maxPlayer)

	client.game = game
	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	return "", false
}

func (client *Client) HandleGAME_CONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	gameName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	game := server.HasGame(gameName)
	if game == nil {
		return "NO_SUCH_GAME", false
	}

	if game.NrClients() == game.MaxClients() {
		return "GAME_FULL", false
	}

	game.AddClient(client)
	client.game = game

	client.SendPacket("GAME_CONNECT", client.remoteIp())

	server.BroadcastToConnectedClients("CLIENTS_UPDATE")

	return "", false
}

func (client *Client) HandleGAME_START(server *Server, pkg *packet.Packet) (string, bool) {
	if client.game == nil {
		client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
		client.Disconnect()
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
		client.state = RECENTLY_DISCONNECTED
		return "", true
	}

	if client.game.Host() != client {
		return "DEFICIENT_PERMISSION", false
	}

	client.SendPacket("GAME_START")

	server.BroadcastToConnectedClients("GAMES_UPDATE")

	return "", false
}

// NOCOM(sirver): must only have one client for each user.
// NOCOM(sirver): add in a _
func (client *Client) HandleGAME_DISCONNECT(server *Server, pkg *packet.Packet) (string, bool) {
	if client.game == nil {
		client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
		client.Disconnect()
		server.BroadcastToConnectedClients("CLIENTS_UPDATE")
		client.state = RECENTLY_DISCONNECTED
		return "", true
	}

	game := client.game
	client.game = nil

	server.BroadcastToConnectedClients("CLIENTS_UPDATE")
	log.Printf("client.Name(): %v\n", client.Name())
	if game.Host() == client {
		log.Printf("The Host() is leaving.")
		server.RemoveGame(game)
		server.BroadcastToConnectedClients("GAMES_UPDATE")
	} else {
		log.Printf("Someone is leaving.")
	}
	game.RemoveClient(client)

	return "", false
}

func (client *Client) HandleCLIENTS(server *Server, pkg *packet.Packet) (string, bool) {
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

func (client *Client) HandleGAMES(server *Server, pkg *packet.Packet) (string, bool) {
	nrGames := server.NrGames()
	data := make([]interface{}, 2+nrGames*3)

	data[0] = "GAMES"
	data[1] = nrGames
	n := 2
	server.ForeachGame(func(game *Game) {
		data[n+0] = game.Name()
		data[n+1] = game.Host().buildId
		data[n+2] = game.State() == CONNECTABLE
		n += 3
	})
	client.SendPacket(data...)

	return "", false
}
