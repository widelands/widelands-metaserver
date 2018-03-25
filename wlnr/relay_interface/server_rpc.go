package relay_interface

import (
	"errors"
	"log"
	"net"
	"net/rpc"
	"time"
)

type ServerRPC struct {
	callback ServerCallback
	client   *rpc.Client
	listener net.Listener
}

type ServerRPCMethods struct {
	server *ServerRPC
}

func NewServerRPC(callback ServerCallback) Server {
	// Start rpc server so the metaserver can tell us about new games
	log.Printf("Starting RPC server")

	server := &ServerRPC{
		callback: callback,
		client:   nil,
	}

	serverMethods := &ServerRPCMethods{
		server: server,
	}
	rpc.Register(serverMethods)
	l, e := net.Listen("tcp", ":7398")
	if e != nil {
		log.Fatal("Unable to listen on rpc port: ", e)
	}
	server.listener = l
	go rpc.Accept(l)
	return server
}

func (server *ServerRPC) CloseConnection() {
	server.listener.Close()
}

func (serverM *ServerRPCMethods) NewGame(in *GameData, success *bool) error {
	log.Printf("NewGame called")
	// The metaserver wants something from us. Try to connect to it, too
	if serverM.server.client == nil {
		connection, err := net.DialTimeout("tcp", "localhost:7399", time.Duration(10)*time.Second)
		if err == nil {
			serverM.server.client = rpc.NewClient(connection)
		}
	}

	ret := serverM.server.callback.CreateGame(in.Name, in.Password)
	if ret != true {
		return errors.New("Game already exists")
	}
	*success = true
	return nil
}

func (server *ServerRPC) GameConnected(name string) {
	if server.client == nil {
		return
	}
	// Tell the metaserver about it
	var ignored bool
	data := GameData{
		Name: name,
	}
	server.client.Call("ClientRPCMethods.GameConnected", data, &ignored)
}

func (server *ServerRPC) GameClosed(name string) {
	if server.client == nil {
		return
	}
	var ignored bool
	data := GameData{
		Name: name,
	}
	server.client.Call("ClientRPCMethods.GameClosed", data, &ignored)
}
