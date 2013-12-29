package main

import (
	"container/list"
	"log"
	"time"
)

const NETCMD_METASERVER_PING = "\x00\x03@"

type GameState int

const (
	NOT_CONNECTABLE GameState = iota
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
	for server.HasGame(game.Name()) == game {
		log.Printf("ALIVE 1\n")
		pinger := server.NewGamePinger(host)
		log.Printf("ALIVE 2\n")

		success, ok := <-pinger.C
		log.Printf("ALIVE 3\n")
		if success && ok {
			log.Printf("ALIVE 4\n")
			if game.state == NOT_CONNECTABLE {
				game.state = CONNECTABLE
				game.Host().SendPacket("GAME_OPEN")
				server.BroadcastToConnectedClients("GAMES_UPDATE")
			}
			time.Sleep(server.GamePingTimeout())
		} else {
			log.Printf("ALIVE 5\n")
			if game.state == NOT_CONNECTABLE {
				game.Host().SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
				server.BroadcastToConnectedClients("GAMES_UPDATE")
			} else {
				server.RemoveGame(game)
			}
		}
		log.Printf("ALIVE 6\n")
	}
}

func NewGame(host *Client, server *Server, gameName string, maxClients int) *Game {
	game := &Game{
		clients:    list.New(),
		name:       gameName,
		maxClients: maxClients,
		state:      NOT_CONNECTABLE,
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
	host := g.clients.Front().Value.(*Client)

	log.Printf("host.Name(): %v\n", host.Name())
	return host
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
