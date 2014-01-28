package main

import (
	"log"

	"github.com/thoj/go-ircevent"
)

type IRCBridger interface {
	Connect(chan IRCMessage) bool
	Quit()
	recieveMessages(chan string)
}

type IRCBridge struct {
	connection                  *irc.Connection
	nick, user, channel, server string
	useTLS                      bool
}

type IRCMessage struct {
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

func (bridge *IRCBridge) Connect(returnChannel chan IRCMessage) bool {
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
		returnChannel <- IRCMessage{nick: event.Nick,
			message: event.Message,
		}
	})
	return true
}

func (bridge *IRCBridge) Quit() {
	bridge.connection.Quit()
}

func (bridge IRCBridge) recieveMessages(messagesToIrc chan string) {
	for {
		bridge.connection.Privmsg(bridge.channel, <-messagesToIrc)
	}
}
