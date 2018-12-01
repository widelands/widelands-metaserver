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

func (g GameState) String() string {
	switch g {
	case INITIAL_SETUP:
		return "INITIAL_SETUP"
	case NOT_CONNECTABLE:
		return "NOT_CONNECTABLE"
	case CONNECTABLE:
		return "CONNECTABLE"
	case RUNNING:
		return "RUNNING"
	default:
		log.Fatalf("Unknown game state: %d", g)
		return "UNKNOWN"
	}
}

type Game struct {
	// The host is also listed in players.
	host      string
	players   map[string]bool
	name      string
	buildId   string
	state     GameState
	usesRelay bool // True if all network traffic passes through our relay server.
	creationTime time.Time
}

type GamePinger struct {
	C chan bool
}

func (game *Game) doPing(server *Server, host string, pingTimeout time.Duration) bool {
	pinger := server.NewGamePinger(host, pingTimeout)
	success, ok := <-pinger.C
	result := success && ok

	if result {
		switch game.state {
		case INITIAL_SETUP, NOT_CONNECTABLE:
			game.SetState(*server, CONNECTABLE)
		case CONNECTABLE, RUNNING:
			// Do nothing
		default:
			log.Fatalf("Unhandled game.state: %v", game.state)
		}
	} else {
		switch game.state {
		case INITIAL_SETUP:
			game.SetState(*server, NOT_CONNECTABLE)
		case NOT_CONNECTABLE:
			// Do nothing.
		case CONNECTABLE:
			game.SetState(*server, NOT_CONNECTABLE)
		case RUNNING:
			// Do nothing
		default:
			log.Fatalf("Unhandled game.state: %v", game.state)
		}
		if !game.usesRelay {
			host := server.HasClient(game.Host())
			if host == nil || host.Game() != game {
				// Can happen if a client has lost connection to the metaserver.
				// In that case, kick all players and remove the game
				log.Printf("Removing unconnectable game '%v' of disconnected legacy player %v", game.Name(), game.Host())
				game.RemovePlayer(game.Host(), server)
				return result
			}
		}
	}
	return result
}

func (game *Game) pingCycle(server *Server) {
	if game.usesRelay {
		log.Fatalf("Error: Started pingCycle for game %v on relay", game.Name())
	}

	pingTimeout := server.GameInitialPingTimeout()
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

		connected := game.doPing(server, host.remoteIp(), pingTimeout)
		if first_ping {
			// On first ping, inform the client about the result
			if connected {
				host.SendPacket("GAME_OPEN")
			} else {
				host.SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
			}
			first_ping = false
		}

		pingTimeout = server.GamePingTimeout()
		time.Sleep(server.GamePingTimeout())
	}
}

func NewGame(host string, buildId string, server *Server, gameName string, shouldUseRelay bool) *Game {
	game := &Game{
		players:      make(map[string]bool),
		host:         host,
		buildId:      buildId,
		name:         gameName,
		state:        INITIAL_SETUP,
		usesRelay:    shouldUseRelay,
		creationTime: time.Now(),
	}
	server.AddGame(game)

	if !shouldUseRelay {
		go game.pingCycle(server)
	}
	return game
}

func (g Game) Name() string {
	return g.name
}

func (g Game) BuildId() string {
	return g.buildId
}

func (g Game) State() GameState {
	return g.state
}
func (g *Game) SetState(server Server, state GameState) {
	if state != g.state {
		g.state = state
		server.BroadcastToConnectedClients("GAMES_UPDATE")
		log.Printf("Game '%v' is now in state %v", g.Name(), g.state.String())
	}
}

func (g Game) Host() string {
	return g.host
}

func (g *Game) AddPlayer(userName string) {
	g.players[userName] = true
}

func (g *Game) RemovePlayer(userName string, server *Server) {
	if userName == g.host {
		if !g.usesRelay {
			log.Printf("Host %v leaves self-hosted game '%v'. This ends the game", userName, g.name)
			server.RemoveGame(g)
		} else {
			log.Printf("Host %v leaves game '%v' on relay", userName, g.name)
		}
		return
	}

	if _, ok := g.players[userName]; ok {
		log.Printf("Client %v leaves game '%v'", userName, g.name)
		g.players[userName] = false
	}
}

func (g Game) NrPlayers() int {
	return len(g.players)
}

func (g Game) UsesRelay() bool {
	return g.usesRelay
}

func (g Game) CreationTime() time.Time {
	return g.creationTime
}
