package main

import (
	"log"

	"github.com/thoj/go-ircevent"
)

type IRCBridge struct {
	nick, user, channel, server string
	connection                  *irc.Connection
	useTLS                      bool
}

func NewIRCBridge(server, nick, user, channel string, usetls bool) *IRCBridge {
	return &IRCBridge{
		nick:    nick,
		user:    user,
		channel: channel,
		server:  server,
		useTLS:  usetls,
	}
}

func (bridge *IRCBridge) connect() {
	//Create new connection
	bridge.connection = irc.IRC(bridge.nick, bridge.user)
	//Set options
	bridge.connection.UseTLS = true //default is false
	//connection.TLSOptions //set ssl options
	//connection.Password = "[server password]"
	//Commands
	err := bridge.connection.Connect(bridge.server) //Connect to server
	if err != nil {
		log.Fatal("Can't connect to freenode.")
	}
	bridge.connection.Join(bridge.channel)
}

func (bridge *IRCBridge) quit() {
	bridge.connection.Quit()
}

func (bridge *IRCBridge) setCallback(callback func(string, string)) {
	bridge.connection.AddCallback("PRIVMSG", func(e *irc.Event) {
		callback(e.Nick, e.Message)
	})
}

func (bridge *IRCBridge) send(m string) {
	bridge.connection.Privmsg(bridge.channel, m)
}
