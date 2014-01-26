package main

import (
	"fmt"

	"github.com/thoj/go-ircevent"
)

type IRCBridge struct {
	nick, user, channel, server string
	ircobj                      *irc.Connection
}

func NewIRCBridge() *IRCBridge {
	return &IRCBridge{
		nick:    "WLMetaServer",
		user:    "WLMetaServer",
		channel: "#widelands-test",
		server:  "irc.freenode.net:7000"}
}

func (bridge *IRCBridge) connect() {
	bridge.ircobj = irc.IRC(bridge.nick, bridge.user) //Create new ircobj
	//Set options
	bridge.ircobj.UseTLS = true //default is false
	//ircobj.TLSOptions //set ssl options
	//ircobj.Password = "[server password]"
	//Commands
	err := bridge.ircobj.Connect(bridge.server) //Connect to server
	if err != nil {
		fmt.Println("Can't connect to freenode.")
	}
	bridge.ircobj.Join(bridge.channel)
	bridge.ircobj.AddCallback("PRIVMSG", privmsg)
}

func privmsg(event *irc.Event) {
	//e.Message contains the message
	fmt.Println(event.Message)
	//e.Nick Contains the sender
	fmt.Println(event.Nick)
	//e.Arguments[0] Contains the channel
	fmt.Println(event.Arguments[0])
}
func (bridge *IRCBridge) send(m string) {
	fmt.Println(m)
	bridge.ircobj.Privmsg(bridge.channel, m)
}
