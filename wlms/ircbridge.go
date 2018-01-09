package main

import (
	"log"
	"github.com/thoj/go-ircevent"
	"strings"
)

// Structure with channels for communication between IRCBridger and the metaserver
type IRCBridgerChannels struct {
	// Messages sent by IRC users that should be displayed in the lobby
	messagesFromIRC  chan Message
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
		messagesToIRC:     make(chan Message, 50),
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
	//Create new connection
	bridge.connection = irc.IRC(bridge.nick, bridge.user)
	//Set options
	bridge.connection.UseTLS = bridge.useTLS
	//connection.TLSOptions //set ssl options
	//connection.Password = "[server password]"
	//Commands
	err := bridge.connection.Connect(bridge.server) //Connect to server
	if err != nil {
		log.Fatalf("Can't connect %s", bridge.server)
		return false
	}
	bridge.connection.AddCallback("001", func(e *irc.Event) { bridge.connection.Join(bridge.channel) })
	bridge.connection.AddCallback("PRIVMSG", func(event *irc.Event) {
		//e.Message contains the message
		//e.Nick Contains the sender
		//e.Arguments[0] Contains the channel
		select {
		case channels.messagesFromIRC <- Message{nick: event.Nick,
			message: event.Message(),
		}:
		default:
			log.Println("IRC Message Queue full.")
		}
	})
	bridge.connection.AddCallback("JOIN", func(e *irc.Event) {
		if e.Nick == bridge.nick {
			return
		}
		select {
		case channels.clientsJoiningIRC <- e.Nick:
		default:
			log.Println("IRC Joining Queue full.")
		}
	})
	bridge.connection.AddCallback("QUIT", func(e *irc.Event) {
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
	go func() {
		for m := range channels.messagesToIRC {
			bridge.connection.Privmsg(bridge.channel, m.message)
		}
	}()
	return true
}

func (bridge *IRCBridge) Quit() {
	bridge.connection.Quit()
}
