package main

import (
	"container/list"
	"io"
	"log"
	"math"
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

	// Whether we are currently shutting down
	currentlyShuttingDown bool
}

func NewGame(name, password string, server *Server) *Game {
	game := &Game{
		host:                  nil,
		clients:               list.New(),
		nextClientId:          ID_HOST + 1,
		protocolVersion:       VERSION_UNKNOWN,
		gameName:              name,
		hostPassword:          password,
		server:                server,
		currentlyShuttingDown: false,
	}
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
		game.DisconnectClient(game.clients.Front().Value.(*Client), "NORMAL")
	}
	game.DisconnectClient(game.host, "NORMAL")
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
	} else {
		// A normal client
		if game.protocolVersion != version {
			client.Disconnect("WRONG_VERSION")
			return
		}
		if game.nextClientId >= 250 {
			// Avoid overflow of uint8 id
			log.Printf("Too many clients in game %v, disconnecting new client", game.Name())
			client.Disconnect("NORMAL")
			return
		}
		client.id = game.nextClientId
		game.nextClientId = game.nextClientId + 1
		game.clients.PushBack(client)
		go game.handleClientMessages(client)
		cmd := NewCommand(kConnectClient)
		cmd.AppendUInt(client.id)
		game.host.SendCommand(cmd)
	}
	cmd := NewCommand(kWelcome)
	cmd.AppendUInt(game.protocolVersion)
	cmd.AppendString(game.gameName)
	client.SendCommand(cmd)
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
		// Admittedly: Shutting down the game is hard. But when the host is sending
		// trash or becomes disconnected there is nothing we can do anyway
		game.Shutdown()
		return
	}
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			if game.host != nil {
				cmd := NewCommand(kDisconnectClient)
				cmd.AppendUInt(client.id)
				game.host.SendCommand(cmd)
			}
			client.Disconnect(reason)
			game.clients.Remove(e)
			break
		}
	}
}

func (game *Game) handlePong(client *Client) {
	seq, err := client.ReadUint8()
	if err != nil {
		game.DisconnectClient(client, "PROTOCOL_VIOLATION")
		return
	}
	client.HandlePong(seq)
}

func (game *Game) addClientRTT(cmd *Command, client *Client) {
	rtt_ms := client.RttLastPing() / time.Millisecond
	time_s := time.Since(client.TimeLastPong()).Seconds()
	cmd.AppendUInt(client.id)
	cmd.AppendUInt(uint8(math.Min(float64(rtt_ms), 255)))
	cmd.AppendUInt(uint8(math.Min(time_s, 255)))
}

func (game *Game) sendRTTs(receiver *Client) {
	cmd := NewCommand(kRoundTripTimeResponse)
	// Count how many non-nil clients we have
	client_count := 0
	if game.host != nil {
		client_count++
	}
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) != nil {
			client_count++
		}
	}
	cmd.AppendUInt(uint8(client_count))
	if game.host != nil {
		game.addClientRTT(cmd, game.host)
	}
	for e := game.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) != nil {
			game.addClientRTT(cmd, e.Value.(*Client))
		}
	}
	receiver.SendCommand(cmd)
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
			cmd := NewCommand(kFromClient)
			cmd.AppendUInt(client.id)
			cmd.AppendBytes(packet)
			game.host.SendCommand(cmd)
		case kDisconnect:
			// Read but ignore the reason
			client.ReadString()
			game.DisconnectClient(client, "NORMAL")
			return
		case kPong:
			game.handlePong(client)
		case kRoundTripTimeRequest:
			game.sendRTTs(client)
		}

	}
}

func (game *Game) handleHostMessages() {
	for {
		// Read for ever until an error occurs or we receive a disconnect
		if game.host == nil {
			// host is nil: Disconnect induced by some other code
			return
		}
		command, err := game.host.ReadUint8()
		if err != nil {
			if err == io.EOF {
				game.DisconnectClient(game.host, "NORMAL")
			} else {
				game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
			}
			return
		}
		switch command {
		case kToClients:
			var destinations []*Client
			for {
				id, err := game.host.ReadUint8()
				if err != nil {
					game.DisconnectClient(game.host, "PROTOCOL_VIOLATION")
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
				return
			}
			cmd := NewCommand(kFromHost)
			cmd.AppendBytes(packet)
			for _, client := range destinations {
				client.SendCommand(cmd)
			}
		case kDisconnect:
			// Read but ignore
			game.host.ReadString()
			game.DisconnectClient(game.host, "NORMAL")
			return
		case kPong:
			game.handlePong(game.host)
		case kRoundTripTimeRequest:
			game.sendRTTs(game.host)
		}
	}
}
