package relay_interface

type Client interface {
	CreateGame(name string, password string) bool
	CloseConnection()
}

type ClientCallback interface {
	GameConnected(name string)
	GameClosed(name string)
}
