package main

import (
	"log"

	"github.com/thoj/go-ircevent"
)

type IRCBridge struct {
	nick, user, channel, server string
	connection                  *irc.Connection
}

func NewIRCBridge() *IRCBridge {
	return &IRCBridge{
		nick:    "WLMetaServer",
		user:    "WLMetaServer",
		channel: "#widelands-test",
		server:  "irc.freenode.net:7000"}
}

func (bridge *IRCBridge) connect() {
	bridge.connection = irc.IRC(bridge.nick, bridge.user) //Create new connection
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
	bridge.connection.AddCallback("PRIVMSG", bridge.privmsg)
}

func (bridge *IRCBridge) quit() {
	bridge.connection.Quit()
}

func (bridge *IRCBridge) privmsg(event *irc.Event) {
	//e.Message contains the message
	log.Println(event.Message)
	//e.Nick Contains the sender
	log.Println(event.Nick)
	//e.Arguments[0] Contains the channel
	log.Println(event.Arguments[0])
}

func (bridge *IRCBridge) send(m string) {
	bridge.connection.Privmsg(bridge.channel, m)
}
