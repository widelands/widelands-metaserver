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

func (server *ServerRPC) connect() bool {
	// Open connection to metaserver
	connection, err := net.DialTimeout("tcp", "localhost:7399", time.Duration(10)*time.Second)
	if err != nil {
		log.Fatal("ServerRPC: Unable to connect to metaserver at localhost: ", err)
		return false
	}
	server.client = rpc.NewClient(connection)
	log.Println("ServerRPC: Connected to metaserver")
	return true
}

func (server *ServerRPC) CloseConnection() {
	server.listener.Close()
}

func (server *ServerRPC) callClientMethod(action, gameName string) {
	if server.client == nil {
		// Probably there never was a connection, try to create one now
		// Isn't done in the constructor since we have a circular dependency between
		// relay and metaserver
		server.connect()
	}
	var ignored bool
	data := GameData{
		Name: gameName,
	}
	for i := 0; i < 2; i++ {
		err := server.client.Call("ClientRPCMethods."+action, data, &ignored)
		if err == nil {
			break
		}
		if err == rpc.ErrShutdown {
			if !server.connect() {
				log.Printf("ServerRPC: Lost connection to metaserver and are unable to reconnect")
				return
			}
			log.Printf("ServerRPC: Lost connection to metaserver but was able to reconnect")
		} else {
			log.Printf("ServerRPC  error: %v", err)
			return
		}
	}

}

func (server *ServerRPC) GameConnected(name string) {
	// Tell the metaserver about it
	server.callClientMethod("GameConnected", name)
}

func (server *ServerRPC) GameClosed(name string) {
	server.callClientMethod("GameClosed", name)
}

func (serverM *ServerRPCMethods) NewGame(in *GameData, success *bool) error {
	ret := serverM.server.callback.CreateGame(in.Name, in.Password)
	if ret != true {
		return errors.New("Game already exists")
	}
	*success = true
	return nil
}
