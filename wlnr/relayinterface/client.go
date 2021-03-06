package relayinterface

type ServerStatus struct {
	NClients        int // does not count IRC users
	NClientsInGames int
	NGames          int // contains nOpenGames
	NOpenGames      int
}

// Client is an interface for communicating with the relay server.
// Implementing structs should forward the method calls in this
// interface to the relay server.
type Client interface {
	// Open a new game on the relay with the given name.
	// The given password protects the host-position in the new game.
	// Fails if there is no relay or the game already exists.
	CreateGame(name string, password string) bool
	// Closes the game on the relay, removing all state of it
	// and closing all network connections.
	// Fails if there is no game with this name.
	RemoveGame(name string) bool
	// Closes connection to the relay.
	CloseConnection()
}

// ClientCallback has to be implemented by classes that should
// receive messages from the relay.
type ClientCallback interface {
	// The relay notifies that a host has connectd to the game with the given name.
	GameConnected(name string)
	// The relay notifies that the game with the given name has been closed on the relay.
	GameClosed(name string)
	// Request the current status, e.g., number of active users and games.
	Status() *ServerStatus
}
