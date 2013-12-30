package main

import (
	"container/list"
	"log"
	"time"
)

type GameState int

const (
	INITIAL_SETUP GameState = iota
	NOT_CONNECTABLE
	CONNECTABLE
)

type Game struct {
	// The first client is always the host.
	clients    *list.List
	name       string
	maxClients int
	state      GameState
}

type GamePinger struct {
	C chan bool
}

func (game *Game) pingCycle(host *Client, server *Server) {
	// Remember to remove the game when we no longer receive pings.
	defer server.RemoveGame(game)

	for {
		// This game is not even in our list anymore. Give up.
		if server.HasGame(game.Name()) != game {
			return
		}
		// If the game has no host anymore or it has disconnected, remove the
		// game.
		host := game.Host()
		if host == nil || server.HasClient(host.Name()) == nil {
			return
		}

		pinger := server.NewGamePinger(host)
		success, ok := <-pinger.C
		if success && ok {
			log.Printf("Successfull ping reply from game %s.", game.Name())
			switch game.state {
			case INITIAL_SETUP:
				host.SendPacket("GAME_OPEN")
				fallthrough
			case NOT_CONNECTABLE:
				server.BroadcastToConnectedClients("GAMES_UPDATE")
			case CONNECTABLE:
				// do nothing
			default:
				log.Fatalf("Unknown game.state: %v", game.state)
			}
			game.state = CONNECTABLE
		} else {
			log.Printf("Failed ping reply from game %s.", game.Name())
			switch game.state {
			case INITIAL_SETUP:
				host.SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
				server.BroadcastToConnectedClients("GAMES_UPDATE")
			case CONNECTABLE:
				return
			case NOT_CONNECTABLE:
				// do nothing
			default:
				log.Fatalf("Unknown game.state: %v", game.state)
			}
			game.state = NOT_CONNECTABLE
		}
		time.Sleep(server.GamePingTimeout())
	}
}

func NewGame(host *Client, server *Server, gameName string, maxClients int) *Game {
	game := &Game{
		clients:    list.New(),
		name:       gameName,
		maxClients: maxClients,
		state:      INITIAL_SETUP,
	}
	game.clients.PushFront(host)
	server.AddGame(game)
	go game.pingCycle(host, server)
	return game
}

func (g Game) Name() string {
	return g.name
}

func (g Game) State() GameState {
	return g.state
}

func (g Game) MaxClients() int {
	return g.maxClients
}

func (g Game) Host() *Client {
	if g.clients.Len() == 0 {
		return nil
	}
	return g.clients.Front().Value.(*Client)
}

func (g *Game) AddClient(client *Client) {
	g.clients.PushBack(client)
}

func (g *Game) RemoveClient(client *Client) {
	for e := g.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			g.clients.Remove(e)
		}
	}
}

func (g Game) NrClients() int {
	return g.clients.Len()
}
