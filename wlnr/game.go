package main

import (
	"container/list"
	"io"
	"log"
	"time"
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

	// The protocol version used for communication. Set on connect of the host
	// and has to be the same for all clients.
	// This has to be specific for a game since there might be a newer
	// version in trunk than in the latest release
	protocolVersion uint8

	// Name of the game. Used to make sure clients connect to the right
	// game. This is guaranteed to be unique by the metaserver.
	gameName string

	// The password which has to be presented by the host to make sure
	// he really is the host
	hostPassword string

	// A reference of the server since we have to tell him when we shut down
	server *Server

	// A timer checking for timeout of the game host
	// On every message from the host it is reset. If it ever triggers,
	// we probably lost the connection.
	hostTimeout *time.Timer

	// Whether we are currently shutting down
	currentlyShuttingDown bool

	// Whether we are waiting for a Pong. Gives the host
	// a bit more time before we consider it lost.
	waitForPong bool
}

func NewGame(name, password string, server *Server) *Game {
	game := &Game{
		host:            nil,
		clients:         list.New(),
		nextClientId:    ID_HOST + 1,
		protocolVersion: VERSION_UNKNOWN,
		gameName:        name,
		hostPassword:    password,
		server:          server,
		// 25 seconds since the GameHost uses a ping interval of 20 seconds anyway
		hostTimeout:           time.NewTimer(time.Second * 25),
		currentlyShuttingDown: false,
		waitForPong:           false,
	}
	go func() {
		for {
			<-game.hostTimeout.C
			if game.host == nil {
				// Seems the game is over
				break
			}
			if game.waitForPong == false {
				// Give the host a chance to react to a ping
				game.waitForPong = true
				game.host.SendCommand(kPing)
				game.hostTimeout.Reset(time.Second * 11)
			} else {
				// Bad luck: Abort the game
				log.Print("Timeout of host, aborting game ", game.gameName)
				game.Shutdown()
				break
			}
		}
	}()
	return game
}

func (game *Game) Name() string {
	return game.gameName
}

func (game *Game) Shutdown() {
	if game.currentlyShuttingDown == true {
		return
	}
	game.currentlyShuttingDown = true
	log.Printf("Shutting down game '%v'\n", game.gameName)
	for game.clients.Len() > 0 {
		game.DisconnectClient(game.clients.Front().Value.(*Client), "RELAY_SHUTDOWN")
	}
	game.DisconnectClient(game.host, "RELAY_SHUTDOWN")
	game.server.RemoveGame(game)
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
		// Send message to metaserver
		game.server.GameConnected(game.Name())
		/* Removed for now. Might be needed in the future but it leads to possible
		   packet loss, so ignore it for now. Not really thought through yet,
		   will probably shutdown the game
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
	if client == nil {
		return
	} else if game.host == client {
		game.host.Disconnect(reason)
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
			break
		}
	}
}

func (game *Game) handleClientMessages(client *Client) {
	for {
		// Read for ever until an error occurres or we receive a disconnect
		command, err := client.ReadUint8()
		if err == io.EOF {
			game.DisconnectClient(client, "NORMAL")
			return
		} else if err != nil {
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
			// TODO(Notabilis): This line might be a problem when there is no host temporarily.
			// Also, what if the old connection is replaced by a new host a few seconds later?
			// We will probably lose packets this way. :/
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
			if err == io.EOF {
				game.DisconnectClient(game.host, "NORMAL")
			} else {
				game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
			}
			// Admittedly: Shutting down the game is hard. But when the host is sending
			// trash or becomes disconnected there is nothing we can do anyway
			game.Shutdown()
			return
		}
		game.hostTimeout.Reset(time.Second * 25)
		game.waitForPong = false
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
				if client != nil {
					// Should always be the case but might not be due to
					// network delays (host did not receive our message yet)
					destinations = append(destinations, client)
				}
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
		case kDisconnect:
			// Read but ignore
			game.host.ReadString()
			game.DisconnectClient(game.host, "NORMAL")
			game.Shutdown()
			return
		case kPong:
			// Aha.
			// As in: No special handling, timer has been reset above
		}
	}
}
