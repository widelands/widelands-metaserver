package main

import (
	"log"
)

type GameState int

const (
	INITIAL_SETUP GameState = iota
	CONNECTABLE
	RUNNING
)

type Game struct {
	// The host is also listed in players.
	host         string
	players      map[string]bool
	name         string
	maxPlayers   int
	state        GameState
	hostPassword string
}

type GamePinger struct {
	C chan bool
}

func NewGame(host string, server *Server, gameName string, maxPlayers int) *Game {
	game := &Game{
		players:    make(map[string]bool),
		host:       host,
		name:       gameName,
		maxPlayers: maxPlayers,
		state:      INITIAL_SETUP,
	}

	game.hostPassword = TODO: Learn this on first connect from the host

	server.AddGame(game)

	// NOCOM(Notabilis): Send this message when the relay confirms the connection
	host_conn := server.HasClient(game.Host())
	host_conn.SendPacket("GAME_OPEN")
	game.SetState(*server, CONNECTABLE)

	return game
}

func (g Game) Name() string {
	return g.name
}

func (g Game) State() GameState {
	return g.state
}

func (g *Game) SetState(server Server, state GameState) {
	if state != g.state {
		g.state = state
		server.BroadcastToConnectedClients("GAMES_UPDATE")
	}
}

func (g Game) MaxPlayers() int {
	return g.maxPlayers
}

func (g Game) Host() string {
	return g.host
}

func (g *Game) AddPlayer(userName string) {
	g.players[userName] = true
}

func (g *Game) RemovePlayer(userName string, server *Server) {
	if userName == g.host {
		log.Printf("%s leaves game %s. This ends the game.", userName, g.name)
		server.RemoveGame(g)
		return
	}

	if _, ok := g.players[userName]; ok {
		log.Printf("%s leaves game %s.", userName, g.name)
		g.players[userName] = false
	}
}

func (g Game) NrPlayers() int {
	return len(g.players)
}

func (g Game) HostPassword() string {
	return g.hostPassword
}
