package main

import (
	"log"
	"time"
	"net"
)

type GameState int

const (
	INITIAL_SETUP GameState = iota
	NOT_CONNECTABLE
	CONNECTABLE_V4
	CONNECTABLE_V6
	CONNECTABLE_BOTH
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

func (game *Game) handlePingResult(server *Server, result bool, is_v4 bool) {
	if result {
		log.Printf("Successfull ping reply from game %s.", game.Name())
		switch game.state {
		case INITIAL_SETUP:
			if is_v4 {
				game.SetState(*server, CONNECTABLE_V4)
			} else {
				game.SetState(*server, CONNECTABLE_V6)
			}
		case NOT_CONNECTABLE:
			if is_v4 {
				game.SetState(*server, CONNECTABLE_V4)
			} else {
				game.SetState(*server, CONNECTABLE_V6)
			}
		case CONNECTABLE_V4:
			if !is_v4 {
				game.SetState(*server, CONNECTABLE_BOTH)
			}
		case CONNECTABLE_V6:
			if is_v4 {
				game.SetState(*server, CONNECTABLE_BOTH)
			}
		case CONNECTABLE_BOTH, RUNNING:
			// Do nothing
		default:
			log.Fatalf("Unhandled game.state: %v", game.state)
		}
	} else {
		log.Printf("Failed ping reply from game %s.", game.Name())
		switch game.state {
		case INITIAL_SETUP:
			game.SetState(*server, NOT_CONNECTABLE)
		case NOT_CONNECTABLE:
			// Do nothing.
		case CONNECTABLE_V4:
			if is_v4 {
				game.SetState(*server, NOT_CONNECTABLE)
			}
		case CONNECTABLE_V6:
			if !is_v4 {
				game.SetState(*server, NOT_CONNECTABLE)
			}
		case CONNECTABLE_BOTH:
			if is_v4 {
				game.SetState(*server, CONNECTABLE_V6)
			} else {
				game.SetState(*server, CONNECTABLE_V4)
			}
		case RUNNING:
			// Do nothing
		default:
			log.Fatalf("Unhandled game.state: %v", game.state)
		}
	}
}

func (game *Game) pingCycle(server *Server) {
	// Remember to remove the game when we no longer receive pings.
	defer server.RemoveGame(game)

	first_ping := true
	ping_primary_ip := true
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

		connected := false

		// The idea is to alternate between pinging the two IP addresses of the client, except for the
		// first round or if there is only one IP address
		if ping_primary_ip || host.otherIp() == "" {
			// Primary IP
			pinger := server.NewGamePinger(host.remoteIp(), pingTimeout)
			success, ok := <-pinger.C
			connected = success && ok
			game.handlePingResult(server, connected, net.ParseIP(host.remoteIp()).To4() != nil)
		}
		if (!ping_primary_ip || first_ping) && host.otherIp() != "" {
			// Secondary IP
			pinger := server.NewGamePinger(host.otherIp(), pingTimeout)
			success, ok := <-pinger.C
			connected = success && ok
			game.handlePingResult(server, connected, net.ParseIP(host.otherIp()).To4() != nil)

		}
		if first_ping {
			// On first ping, inform the client about the result
			if connected {
				host.SendPacket("GAME_OPEN")
			} else {
				host.SendPacket("ERROR", "GAME_OPEN", "GAME_TIMEOUT")
			}
			first_ping = false
		}

		ping_primary_ip = !ping_primary_ip
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
