package main

import (
	"log"
	"time"
)

type GameState int

const (
	INITIAL_SETUP GameState = iota
	NOT_CONNECTABLE
	CONNECTABLE
	RUNNING
)

type Game struct {
	// The host is also listed in players.
	host       string
	players    map[string]bool
	name       string
	maxPlayers int
	state      GameState
}

type GamePinger struct {
	C chan bool
}

func (game *Game) pingCycle(server *Server) {
	// Remember to remove the game when we no longer receive pings.
	defer server.RemoveGame(game)

	first_ping := true
	for {
		// This game is not even in our list anymore. Give up. If the game has no
		// host anymore or it has disconnected, remove the game.
		if server.HasGame(game.Name()) != game || len(game.players) == 0 {
			return
		}
		host := server.HasClient(game.Host())
		if host == nil {
			return
		}
		pingTimeout := server.GamePingTimeout()
		if first_ping {
			pingTimeout = server.GameInitialPingTimeout()
		}
		first_ping = false

		pinger := server.NewGamePinger(host, pingTimeout)
		success, ok := <-pinger.C
		if success && ok {
			log.Printf("Successfull ping reply from game %s.", game.Name())
			switch game.state {
			case INITIAL_SETUP:
				host.SendPacket("GAME_OPEN")
				game.SetState(*server, CONNECTABLE)
			case NOT_CONNECTABLE:
				game.SetState(*server, CONNECTABLE)
			case CONNECTABLE, RUNNING:
				// Do nothing
			default:
				log.Fatalf("Unhandled game.state: %v", game.state)
			}
		} else {
			log.Printf("Failed ping reply from game %s.", game.Name())
			switch game.state {
			case INITIAL_SETUP:
				host.SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
				game.SetState(*server, NOT_CONNECTABLE)
			case NOT_CONNECTABLE:
				// Do nothing.
			case CONNECTABLE, RUNNING:
				return
			default:
				log.Fatalf("Unhandled game.state: %v", game.state)
			}
		}
		time.Sleep(server.GamePingTimeout())
	}
}

func NewGame(host string, server *Server, gameName string, maxPlayers int) *Game {
	game := &Game{
		players:    make(map[string]bool),
		host:       host,
		name:       gameName,
		maxPlayers: maxPlayers,
		state:      INITIAL_SETUP,
	}
	server.AddGame(game)

	go game.pingCycle(server)
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
