package wlnr

import (
	"container/list"
	"fmt"
	"github.com/widelands_metaserver/wlms/packet"
	"log"
	"reflect"
	"strings"
	"time"
)

// ID inside the Client structure which denotes the host
const ID_HOST = 0

// The used protocol version is not known since the host has not yet connected
const VERSION_UNKNOWN = 0

// Structure to bundle the TCP connection with its packet buffer
type Client struct {
	// The TCP connection to the client
	conn ReadWriteCloserWithIp

	// We always read one whole packet
	dataStream chan *packet.Packet

	// The id of this client when refering to him in messages to the host
	id int
}

func (client *Client) readingLoop() {
	defer close(client.dataStream)
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			return
		}
		client.dataStream <- pkg
	}
}

type Game struct {
	// The connection (net.Conn most likely) that let us talk to the game host
	host *Client

	// Game clients and observers. For the relay there is not difference
	// between them
	clients *list.List

	// The id the next client will get assigned
	nextClientId int

	// A list of all TCP channels currently in use
	channels []reflect.SelectCase

	// A kind of map to link the indices inside the slice to clients
	channelsToClients [](*Client)

	// The protocol version used for communication. Set on connect of the host
	// and has to be the same for all clients.
	// Being able to support different versions per client might be nice
	// but network games break with most commits anyway and the protocol
	// version only seldom changes.
	protocolVersion int

	// Name of the game. Used to make sure clients connect to the right
	// game. This is guaranteed to be unique by the metaserver.
	// TODO(Notabilis): Make sure it really is guaranteed
	gameName string

	// The password which has to be presented by the host to make sure
	// he really is the host 
	hostPassword string
}
func NewGame(name, password string) *Game {
	game := &Game{
		host:              nil,
		clients:           list.New(),
		nextClientId:      ID_HOST + 1,
		channels:          nil,
		channelsToClients: nil,
		protocolVersion:   VERSION_UNKNOWN,
		gameName:          name,
		hostPassword:      password,
	}
	game.updateChannels()
	go game.mainLoop()
	return game
}

func (game *Game) Name() string {
	return game.gameName
}

func (game *Game) Shutdown(server Server) {
	for game.clients.Len() > 0 {
		game.DisconnectClient(game.clients.Front().Value.(*Client), "RELAY_SHUTDOWN")
	}
	game.DisconnectClient(game.host, "RELAY_SHUTDOWN")
}


func (game *Game) addClient(connection ReadWriteCloserWithIp) {
	client := &Client{
			conn:        connection,
			dataStream:  make(chan *packet.Packet, 10),
			id:          game.nextClientId,
		}
	game.nextClientId = game.nextClientId + 1
	go client.readingLoop()
	game.clients.PushBack(client)
	game.updateChannels()
}

func (game *Game) DisconnectClient(client *Client, reason string) {
	if game.host == client {
		SendPacket(client.conn, "DISCONNECT", reason)
		client.conn.Close()
		game.host = nil
		return
	}
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			SendPacket(client.conn, "DISCONNECT", reason)
			client.conn.Close()
			game.clients.Remove(e)
		}
	}
	game.updateChannels()
}

func (game *Game) updateChannels() {
	game.channels = make([]reflect.SelectCase, game.clients.Len() + 1)
	game.channelsToClients = make([](*Client), game.clients.Len() + 1)
	game.channels = append(game.channels, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(game.host.dataStream),
		})
	game.channelsToClients = append(game.channelsToClients, game.host)
	for e := game.clients.Front(); e != nil; e = e.Next() {
		game.channels = append(game.channels, reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(e.Value.(*Client).dataStream),
			})
		game.channelsToClients = append(game.channelsToClients, e.Value.(*Client))

	}
}

func (game *Game) mainLoop() {
	for len(game.channels) > 0 {
		// Do a select over all our connections
		index, value, ok := reflect.Select(game.channels)
		// Get the Client object associated to the read channel
		client := game.channelsToClients[index]

		if !ok {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}

		pkg := value.Interface().(*packet.Packet)
		cmdName, err := pkg.ReadString()
		if err != nil {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}

		// Find the handler function for this packet
		handlerFunc1 := reflect.ValueOf(game).MethodByName(strings.Join([]string{"Handle_", cmdName}, ""))
		if !handlerFunc1.IsValid() {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}

		handlerFunc2 := handlerFunc1.Interface().(func(*packet.Packet))
		// Call the handler
		handlerFunc2(pkg)
	}
}

// Following code not reworked yet.

func (game *Game) Handle_DISCONNECT(server *Server, pkg *packet.Packet) CmdError {
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

func (c *Client) Handle_LOGIN(server *Server, pkg *packet.Packet) CmdError {
	var isRegisteredOnServer bool
	if err := pkg.Unpack(&c.protocolVersion, &c.userName, &c.buildId, &isRegisteredOnServer); err != nil {
		return CriticalCmdPacketError{err.Error()}
	}

	if c.protocolVersion != 0 && c.protocolVersion != 1 {
		return CriticalCmdPacketError{"UNSUPPORTED_PROTOCOL"}
	}

	if isRegisteredOnServer || c.protocolVersion == 1 {
		nonce, err := pkg.ReadString()
		if err != nil {
			return CriticalCmdPacketError{err.Error()}
		}
		c.nonce = nonce
	}

	if isRegisteredOnServer {
		if server.HasClient(c.userName) != nil {
			return CriticalCmdPacketError{"ALREADY_LOGGED_IN"}
		}
		if !server.UserDb().ContainsName(c.userName) {
			return CriticalCmdPacketError{"WRONG_PASSWORD"}
		}
		if !server.UserDb().PasswordCorrect(c.userName, c.nonce) {
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

