package main

import (
	"log"

	"github.com/thoj/go-ircevent"
)

type IRCBridger interface {
	Connect(chan Message, chan Message) bool
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

func (bridge *IRCBridge) Connect(messagesIn chan Message, messagesOut chan Message) bool {
	//Create new connection
	bridge.connection = irc.IRC(bridge.nick, bridge.user)
	//Set options
	bridge.connection.UseTLS = bridge.useTLS
	//connection.TLSOptions //set ssl options
	//connection.Password = "[server password]"
	//Commands
	err := bridge.connection.Connect(bridge.server) //Connect to server
	if err != nil {
		log.Fatal("Can't connect %s", bridge.server)
		return false
	}
	bridge.connection.Join(bridge.channel)
	bridge.connection.AddCallback("PRIVMSG", func(event *irc.Event) {
		//e.Message contains the message
		//e.Nick Contains the sender
		//e.Arguments[0] Contains the channel
		select {
		case messagesOut <- Message{nick: event.Nick,
			message: event.Message(),
		}:
		default:
			log.Println("Message Queue full.")
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
