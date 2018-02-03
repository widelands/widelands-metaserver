package main

import (
	"log"
	"net"
	"time"
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

func (g GameState) String() string {
	switch g {
	case INITIAL_SETUP:
		return "INITIAL_SETUP"
	case NOT_CONNECTABLE:
		return "NOT_CONNECTABLE"
	case CONNECTABLE_V4:
		return "CONNECTABLE_V4"
	case CONNECTABLE_V6:
		return "CONNECTABLE_V6"
	case CONNECTABLE_BOTH:
		return "CONNECTABLE_BOTH"
	case RUNNING:
		return "RUNNING"
	default:
		log.Fatalf("Unknown game state: %d", g)
		return "UNKNOWN"
	}
}

type Game struct {
	// The host is also listed in players.
	host       string
	players    map[string]bool
	name       string
	buildId    string
	maxPlayers int
	state      GameState
	usesRelay  bool // True if all network traffic passes through our relay server.
}

type GamePinger struct {
	C chan bool
}

func (game *Game) doPing(server *Server, host string, pingTimeout time.Duration) bool {
	pinger := server.NewGamePinger(host, pingTimeout)
	success, ok := <-pinger.C
	result := success && ok
	state_to_check := CONNECTABLE_V4
	if net.ParseIP(host).To4() == nil {
		// If it isn't a IPv4 address, it must be a IPv6 address
		state_to_check = CONNECTABLE_V6
	}

	if result {
		switch game.state {
		case INITIAL_SETUP:
			game.SetState(*server, state_to_check)
		case NOT_CONNECTABLE:
			game.SetState(*server, state_to_check)
		case CONNECTABLE_V4:
			if state_to_check == CONNECTABLE_V6 {
				game.SetState(*server, CONNECTABLE_BOTH)
			}
		case CONNECTABLE_V6:
			if state_to_check == CONNECTABLE_V4 {
				game.SetState(*server, CONNECTABLE_BOTH)
			}
		case CONNECTABLE_BOTH, RUNNING:
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
		case CONNECTABLE_V4:
			if state_to_check == CONNECTABLE_V4 {
				game.SetState(*server, NOT_CONNECTABLE)
			}
		case CONNECTABLE_V6:
			if state_to_check == CONNECTABLE_V6 {
				game.SetState(*server, NOT_CONNECTABLE)
			}
		case CONNECTABLE_BOTH:
			if state_to_check == CONNECTABLE_V4 {
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
	return result
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
			connected = game.doPing(server, host.remoteIp(), pingTimeout)
		}
		if (!ping_primary_ip || first_ping) && host.otherIp() != "" {
			// Secondary IP
			connected = game.doPing(server, host.otherIp(), pingTimeout) || connected
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

// TODO(Notabilis): Remove all this useless maxPlayers stuff or find a use for it. Currently its always 1024.
func NewGame(host string, buildId string, server *Server, gameName string, maxPlayers int, shouldUseRelay bool) *Game {
	game := &Game{
		players:    make(map[string]bool),
		host:       host,
		buildId:    buildId,
		name:       gameName,
		maxPlayers: maxPlayers,
		state:      INITIAL_SETUP,
		usesRelay:  shouldUseRelay,
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
		log.Printf("Host %v leaves game '%v'. This ends the game", userName, g.name)
		server.RemoveGame(g)
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
