package relay_interface

import (
	"log"
	"net"
	"net/rpc"
	"time"
)

type ClientRPC struct {
	callback ClientCallback
	relay    *rpc.Client
	listener net.Listener
}

type ClientRPCMethods struct {
	client *ClientRPC
}

func NewClientRPC(callback ClientCallback) Client {
	client := &ClientRPC{
		callback: callback,
	}

	if !client.connect() {
		return nil
	}

	// Open our rpc server
	rpcLn, err := net.Listen("tcp", ":7399")
	if err != nil {
		log.Fatal("Error when listening for RPC calls: ", err)
		return nil
	}
	client.listener = rpcLn

	// Run our rpc server
	clientMethods := &ClientRPCMethods{
		client: client,
	}

	rpc.Register(clientMethods)
	go rpc.Accept(rpcLn)

	return client
}

func (client *ClientRPC) connect() bool {
	// Open connection to relay server
	connection, err := net.DialTimeout("tcp", "localhost:7398", time.Duration(10)*time.Second)
	if err != nil {
		log.Fatal("Unable to connect to relay server at localhost: ", err)
		return false
	}
	client.relay = rpc.NewClient(connection)
	log.Println("Connected to relay server")
	return true
}

func (client *ClientRPC) CloseConnection() {
	client.listener.Close()
}

// CreateGame tells the relay server to start a game with the given name.
// The host position in the game is protected by the given password
func (client *ClientRPC) CreateGame(name string, hostPassword string) bool {
	// Tell relay to host game
	success := false
	data := GameData{
		Name:     name,
		Password: hostPassword,
	}
	for i := 0; i < 2; i++ {
		err := client.relay.Call("ServerRPCMethods.NewGame", data, &success)
		if err == nil {
			break
		}
		if err == rpc.ErrShutdown {
			if !client.connect() {
				log.Printf("ClientRPC: Lost connection to relay and are unable to reconnect")
				return false
			}
			log.Printf("ClientRPC: Lost connection to relay but was able to reconnect")
		} else {
			log.Printf("ClientRPC  error: %v", err)
			return false
		}
	}
	return success
}

func (client *ClientRPCMethods) GameConnected(in *GameData, response *bool) (err error) {
	client.client.callback.GameConnected(in.Name)
	return nil
}

func (client *ClientRPCMethods) GameClosed(in *GameData, response *bool) (err error) {
	client.client.callback.GameClosed(in.Name)
	return nil
}
