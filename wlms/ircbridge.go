package main

import (
	"github.com/thoj/go-ircevent"
	"log"
	"strings"
)

// Structure with channels for communication between IRCBridger and the metaserver
type IRCBridgerChannels struct {
	// Messages sent by IRC users that should be displayed in the lobby
	messagesFromIRC chan Message
	// Messages from players in the lobby that should be relayed to IRC
	messagesToIRC chan Message
	// Clients joining the IRC channel that should be added to the client list in the lobby
	clientsJoiningIRC chan string
	// Clients leaving the IRC channel
	clientsLeavingIRC chan string
}

func NewIRCBridgerChannels() *IRCBridgerChannels {
	return &IRCBridgerChannels{
		messagesFromIRC:   make(chan Message, 50),
		messagesToIRC:     make(chan Message, 5),
		clientsJoiningIRC: make(chan string, 50),
		clientsLeavingIRC: make(chan string, 50),
	}
}

type IRCBridger interface {
	Connect(*IRCBridgerChannels) bool
	Quit()
}

type IRCBridge struct {
	connection                  *irc.Connection
	nick, user, channel, server string
	useTLS                      bool
}

type Message struct {
	message, nick string
}

func NewIRCBridge(server, realname, nickname, channel string, tls bool) *IRCBridge {
	return &IRCBridge{
		server:  server,
		user:    realname,
		nick:    nickname,
		channel: channel,
		useTLS:  tls,
	}
}

func (bridge *IRCBridge) Connect(channels *IRCBridgerChannels) bool {
	if bridge.nick == "" || bridge.user == "" {
		log.Fatalf("Can't start IRC: nick (%s) or user (%s) invalid", bridge.nick, bridge.user)
		return false
	}
	//Create new connection
	bridge.connection = irc.IRC(bridge.nick, bridge.user)
	if bridge.connection == nil {
		log.Fatalf("Can't create IRC connection")
		return false
	}
	//Set options
	bridge.connection.UseTLS = bridge.useTLS
	//connection.TLSOptions //set ssl options
	//connection.Password = "[server password]"
	//Commands
	err := bridge.connection.Connect(bridge.server) //Connect to server
	if err != nil {
		log.Fatalf("Can't connect to IRC server at %s", bridge.server)
		return false
	}
	bridge.connection.AddCallback("001", func(e *irc.Event) {
		bridge.connection.Join(bridge.channel)
		// HACK: This will start a new goroutine each time we connect to the IRC server.
		// Unfortunately, on connection loss Privmsg() will store a few messages inside a
		// buffer of the IRC library and then block when the buffer runs full.
		// Since the buffer is never emptied by the library but a new one is created,
		// our goroutine is stuck forever. Since we can't un-stuck it, create a new one
		// for the next connection to IRC.
		// The problem is that we end up with a few blocked goroutines over the runtime
		// of the metaserver. But luckily restarts of the IRC server seem to be very rare
		go func() {
			for m := range channels.messagesToIRC {
				bridge.connection.Privmsg(bridge.channel, m.message)
			}
		}()

	})
	bridge.connection.AddCallback("PRIVMSG", func(event *irc.Event) {
		//e.Message contains the message
		//e.Nick Contains the sender
		//e.Arguments[0] Contains the channel
		select {
		case channels.messagesFromIRC <- Message{nick: event.Nick,
			message: event.Message(),
		}:
		default:
			log.Println("Message queue from IRC full.")
		}
	})
	bridge.connection.AddCallback("JOIN", func(e *irc.Event) {
		// An IRC user is joining our channel
		if e.Nick == bridge.nick {
			return
		}
		select {
		case channels.clientsJoiningIRC <- e.Nick:
		default:
			log.Println("IRC Joining Queue full.")
		}
	})
	bridge.connection.AddCallback("PART", func(e *irc.Event) {
		// An IRC user is leaving the channel but stays connected to the IRC server
		if e.Nick == bridge.nick {
			return
		}
		select {
		case channels.clientsLeavingIRC <- e.Nick:
		default:
			log.Println("IRC Quitting Queue full.")
		}
	})

	bridge.connection.AddCallback("QUIT", func(e *irc.Event) {
		// An IRC user closes the connection to the IRC server
		if e.Nick == bridge.nick {
			return
		}
		select {
		case channels.clientsLeavingIRC <- e.Nick:
		default:
			log.Println("IRC Quitting Queue full.")
		}
	})
	// NAMREPLY: List of all nicknames in the channel. Send when we join
	bridge.connection.AddCallback("353", func(e *irc.Event) {
		nicks := strings.Fields(e.Message())
		for _, nick := range nicks {
			if nick == bridge.nick {
				continue
			}
			select {
			case channels.clientsJoiningIRC <- nick:
			default:
				log.Println("IRC Joining Queue full.")
			}
		}
	})
	bridge.connection.AddCallback("*", func(e *irc.Event) {
		// Wildcard event: Is triggered for every even
		// Log the event and its data. Will probably create a lot of
		// noise in the logfile but will hopefully tell me which
		// command creates the IRC "ghosts" (no longer online IRC users,
		// that are still listed on the metaserver)
		switch e.Code {
			case
				"001",
				"PRIVMSG",
				"JOIN",
				"PART",
				"QUIT",
				"353":
				// Skip the messages we are already handling
				return
		}
		log.Printf("IRC DEBUG: Received event %v", e)
	})
	// Main loop to react to disconnects and automatically reconnect
	go bridge.connection.Loop()
	log.Printf("IRC bridge started")
	return true
}

func (bridge *IRCBridge) Quit() {
	bridge.connection.Quit()
}
