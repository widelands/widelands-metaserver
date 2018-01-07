package main

import (
	"log"
	"github.com/thoj/go-ircevent"
	"strings"
)

type IRCBridger interface {
	Connect(chan Message, chan Message, chan string, chan string) bool
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

func (bridge *IRCBridge) Connect(messagesIn chan Message, messagesOut chan Message, clientsJoin chan string, clientsQuit chan string) bool {
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
		case messagesOut <- Message{nick: event.Nick,
			message: event.Message(),
		}:
		default:
			log.Println("IRC Message Queue full.")
		}
	})
	bridge.connection.AddCallback("JOIN", func(e *irc.Event) {
		if e.Nick != bridge.nick {
			select {
			case clientsJoin <- e.Nick:
			default:
				log.Println("IRC Joining Queue full.")
			}
		}

	})
	bridge.connection.AddCallback("QUIT", func(e *irc.Event) {
		if e.Nick != bridge.nick {
			select {
			case clientsQuit <- e.Nick:
			default:
				log.Println("IRC Quitting Queue full.")
			}
		}
	})
	// NAMREPLY: List of all nicknames in the channel. Send when we join
	bridge.connection.AddCallback("353", func(e *irc.Event) {
		nicks := strings.Fields(e.Message())
		for _, nick := range nicks {
			if nick != bridge.nick {
				select {
				case clientsJoin <- nick:
				default:
					log.Println("IRC Joining Queue full.")
				}
			}
		}
	})
	go func() {
		for m := range messagesIn {
			bridge.connection.Privmsg(bridge.channel, m.message)
		}
	}()
	return true
}

func (bridge *IRCBridge) Quit() {
	bridge.connection.Quit()
}
