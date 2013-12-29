package main

import (
	"container/list"
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
	Send        chan string
	Recv        chan string
	PingTimeout time.Duration
}

func (game *Game) pingCycle(pinger *GamePinger, server *Server) {
	timer := time.NewTimer(0) // fires immediately
	waitingForPong := false
	for done := false; !done; {
		select {
		case data := <-pinger.Recv:
			if data == NETCMD_METASERVER_PING {
				waitingForPong = false
				if game.state == NOT_CONNECTABLE {
					game.state = CONNECTABLE
					game.Host().SendPacket("GAME_OPEN")
					server.BroadcastToConnectedClients("GAMES_UPDATE")
				}
				timer.Reset(pinger.PingTimeout)
				// NOCOM(sirver): Change type of game to verified?
			} else {
				done = true
			}
		case <-timer.C:
			if !waitingForPong {
				pinger.Send <- NETCMD_METASERVER_PING
				waitingForPong = true
				timer.Reset(pinger.PingTimeout)
			} else {
				if game.state == NOT_CONNECTABLE {
					game.Host().SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
					done = true
				} else {
					server.RemoveGame(game)
				}
			}
		}
	}
	server.BroadcastToConnectedClients("GAMES_UPDATE")
}

func NewGame(host *Client, server *Server, gameName string, maxClients int) *Game {
	game := Game{
		clients:    list.New(),
		name:       gameName,
		maxClients: maxClients,
		state:      NOT_CONNECTABLE,
	}
	game.clients.PushFront(host)
	go game.pingCycle(server.NewGamePinger(host), server)
	return &game
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
