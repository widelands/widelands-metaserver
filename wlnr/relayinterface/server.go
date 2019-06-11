package relayinterface

// The Server interface describes the notifications that can be send to a
// connected metaserver instance.
type Server interface {
	// Notify metaserver that a host connected to a game.
	GameConnected(name string)
	// Notify metaserver that a game has ended.
	GameClosed(name string)
	// Closes the connection to metaserver.
	CloseConnection()
}

// ServerCallback contains methods that are called when
// the metaserver sends a command.
type ServerCallback interface {
	CreateGame(name string, password string) bool
	RemoveGame(name string) bool
}
