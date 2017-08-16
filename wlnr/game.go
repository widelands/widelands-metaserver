package main

import (
	"container/list"
//	"fmt"
//	"github.com/widelands_metaserver/wlms/packet"
//	"log"
//	"net"
//	"reflect"
//	"strings"
//	"strconv"
//	"time"
)

// ID inside the Client structure which denotes the host
const ID_HOST = 1

// The used protocol version is not known since the host has not yet connected
const VERSION_UNKNOWN = 0

type Game struct {
	// The connection (net.Conn most likely) that let us talk to the game host
	host *Client

	// Game clients and observers. For the relay there is not difference
	// between them
	clients *list.List

	// The id the next client will get assigned
	nextClientId uint8

	// A list of all TCP channels currently in use
//	channels []reflect.SelectCase

	// A kind of map to link the indices inside the slice to clients
//	channelsToClients [](*Client)

	// The protocol version used for communication. Set on connect of the host
	// and has to be the same for all clients.
	// Being able to support different versions per client might be nice
	// but network games break with most commits anyway and the protocol
	// version only seldom changes.
	// This has to be specific for a game since there might be a newer
	// version in trunk than in the latest release
	protocolVersion uint8

	// Name of the game. Used to make sure clients connect to the right
	// game. This is guaranteed to be unique by the metaserver.
	// NOCOM(Notabilis): Make sure it really is guaranteed
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
		//channels:          nil,
		//channelsToClients: nil,
		protocolVersion:   VERSION_UNKNOWN,
		gameName:          name,
		hostPassword:      password,
	}
	//game.updateChannels()
	//go game.mainLoop()
	return game
}

func (game *Game) Name() string {
	return game.gameName
}

func (game *Game) Shutdown() {
	for game.clients.Len() > 0 {
		game.DisconnectClient(game.clients.Front().Value.(*Client), "RELAY_SHUTDOWN")
	}
	game.DisconnectClient(game.host, "RELAY_SHUTDOWN")
}


func (game *Game) addClient(client *Client, version uint8, password string) {
	if game.host == nil {
		// First connection to this game / no host yet
		if password != game.hostPassword {
			client.Disconnect("NO_HOST")
			return
		}
		game.protocolVersion = version
		game.host = client
		game.host.id = ID_HOST
		go game.handleHostMessages()
// NOCOM(Notabilis): Detect loss of clients/host and quit game on host loss
	/* Taken out for now. Might be needed in the future but it creates possible
	   packet loss, so ignore it for now
	} else if password == game.hostPassword {
		// Seems like host reconnects, drop the old one
		if game.protocolVersion != version {
			client.Disconnect("WRONG_VERSION")
			return
		}
		game.host.Disconnect("NORMAL")
		game.host = client
		game.host.id = ID_HOST
		go game.handleHostMessages()*/
	} else {
		// A normal client
		if game.protocolVersion != version {
			client.Disconnect("WRONG_VERSION")
			return
		}
		client.id = game.nextClientId
		game.nextClientId = game.nextClientId + 1
		game.clients.PushBack(client)
		go game.handleClientMessages(client)
		game.host.SendCommand(kConnectClient, client.id)
	}
	client.SendCommand(kWelcome, game.protocolVersion, game.gameName)
	//game.updateChannels()
}

func (game *Game) getClient(id uint8) *Client {
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client).id == id {
			return e.Value.(*Client)
		}
	}
	return nil
}

func (game *Game) DisconnectClient(client *Client, reason string) {
	// NOCOM: Are the go routines terminated when disconnecting the client? Guess read*() returns an error then
	if game.host == client {
		client.Disconnect(reason)
		game.host = nil
		return
	}
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			if game.host != nil {
				game.host.SendCommand(kDisconnectClient, client.id)
			}
			client.Disconnect(reason)
			game.clients.Remove(e)
		}
	}
	//game.updateChannels()
}

func (game *Game) handleClientMessages(client *Client) {
	for {
		// Read for ever until an error occurres or we receive a disconnect
		command, err := client.ReadUint8()
		if err != nil {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			return
		}
		switch command {
		case kToHost:
			packet, err := client.ReadPacket()
			if err != nil {
				game.DisconnectClient(client, "PROTOCOL_VIOLATION")
				return
			}
			if client == nil {
				game.DisconnectClient(game.host, "INVALID_ID")
			}
			// TODO(Notabilis): This line might be a problem when there is no host temporarily. Also, what
			// if the old connection is replaced in the near future? We will probably lose packets this way. :/
			game.host.SendCommand(kFromClient, client.id, packet)
		case kDisconnect:
			// Read but ignore the reason
			client.ReadString()
			game.DisconnectClient(client, "NORMAL")
			return
		}

	}
}

func (game *Game) handleHostMessages() {
	for {
		// Read for ever until an error occurres or we receive a disconnect
		command, err := game.host.ReadUint8()
		if err != nil {
			game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
			// Admittedly: Shutting down the relay is hard. But when the host is sending
			// trash there is nothing we can do anyway
			game.Shutdown()
			return
		}
		switch command {
		case kToClients:
			var destinations []*Client
			for {
				id, err := game.host.ReadUint8()
				if err != nil {
					game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
					game.Shutdown()
					return
				}
				if id == 0 {
					break
				}
				client := game.getClient(id)
				if client == nil {
					game.DisconnectClient(game.host, "INVALID_CLIENT")
					game.Shutdown()
					return

				}
				destinations = append(destinations, client)
			}
			packet, err := game.host.ReadPacket()
			if err != nil {
				game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
				game.Shutdown()
				return
			}
			for _, client := range destinations {
				client.SendCommand(kFromHost, packet)
			}
		case kBroadcast:
			packet, err := game.host.ReadPacket()
			if err != nil {
				game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
				game.Shutdown()
				return
			}
			for e := game.clients.Front(); e != nil; e = e.Next() {
				e.Value.(*Client).SendCommand(kFromHost, packet)
			}
		case kDisconnect:
			// Read but ignore
			game.host.ReadString()
			game.DisconnectClient(game.host, "NORMAL")
			game.Shutdown()
			return
		}

	}
}


/*
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
// NOCOM Does it make sense using $index when $ok is false?
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}
/*
		pkg := value.Interface().(*packet.Packet)
		cmdCode, err := pkg.Read()
		if err != nil {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}

		// Find the handler function for this packet
		handlerFunc1 := reflect.ValueOf(game).MethodByName(strings.Join([]string{"Handle_", COMMAND_CODES[cmdCode]}, ""))
		if !handlerFunc1.IsValid() {
			game.DisconnectClient(client, "PROTOCOL_VIOLATION")
			continue
		}

		handlerFunc2 := handlerFunc1.Interface().(func(*packet.Packet))
		// Call the handler
		handlerFunc2(pkg)
		*/
/*	}
}
*/
// Following code not reworked yet.
// TODO Implement handlers

/*

1 : "HELLO", // We actually do not see this command inside this class since its handled by the server
	 2 : "WELCOME",
	 3 : "DISCONNECT",
	 // Host <-> Relay
	 11 : "CONNECT_CLIENT",
	 12 : "DISCONNECT_CLIENT",
	 13 : "TO_CLIENT",
	 14 : "FROM_CLIENT",
	 15 : "BROADCAST",
	 // Client <-> Relay
	 21 : "TO_HOST",
	 22 : "FROM_HOST",

*/
/*
func (game *Game) Handle_DISCONNECT(pkg *packet.Packet) {
	
}

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
*/
