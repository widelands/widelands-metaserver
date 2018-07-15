package relayinterface

import (
	"errors"
	"log"
	"net"
	"net/rpc"
	"time"
)

// ServerRPC implements the server part of a rpc connection between
// metaserver and relay server.
type ServerRPC struct {
	callback ServerCallback
	client   *rpc.Client
	listener net.Listener
}

// ServerRPCMethods is a helper structure for the exposed rpc methods
// of the ServerRPC.
type ServerRPCMethods struct {
	server *ServerRPC
}

// NewServerRPC creates a struct that implements relayinterface.Server over RPC.
// Opens an RPC server running on port 7398.
// Methods of the given callback are called with notifications of the client.
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
		log.Printf("Unable to listen on rpc port: %v", e)
	}
	server.listener = l
	go rpc.Accept(l)
	return server
}

// Establishes a connection to the metaserver.
func (server *ServerRPC) connect() bool {
	// Open connection to metaserver
	connection, err := net.DialTimeout("tcp", "localhost:7399", time.Duration(10)*time.Second)
	if err != nil {
		log.Printf("ServerRPC: Unable to connect to metaserver at localhost: %v", err)
		return false
	}
	server.client = rpc.NewClient(connection)
	log.Println("ServerRPC: Connected to metaserver")
	return true
}

// CloseConnection terminates the connection to the metaserver.
func (server *ServerRPC) CloseConnection() {
	server.listener.Close()
}

// Calls a method on the rpc client.
// (Re-)Connects to the client if currently not connected or the connection is broken.
func (server *ServerRPC) callClientMethod(action, gameName string) {
	if server.client == nil {
		// Probably there never was a connection, try to create one now
		// Isn't done in the constructor since we have a circular dependency between
		// relay and metaserver
		if (!server.connect()) {
			return
		}
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

// GameConnected informs the metaserver that a host connected to a game.
func (server *ServerRPC) GameConnected(name string) {
	// Tell the metaserver about it
	server.callClientMethod("GameConnected", name)
}

// GameClosed informs the metaserver that a game has ended.
func (server *ServerRPC) GameClosed(name string) {
	server.callClientMethod("GameClosed", name)
}

// NewGame is called by the rpc server when the metaserver wants to start a new game.
// Calls the respective method of the ServerCallback given on construction.
func (serverM *ServerRPCMethods) NewGame(in *GameData, success *bool) error {
	ret := serverM.server.callback.CreateGame(in.Name, in.Password)
	if ret != true {
		return errors.New("Game already exists")
	}
	*success = true
	return nil
}
