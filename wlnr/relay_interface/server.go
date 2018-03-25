package relay_interface

type Server interface {
	GameConnected(name string)
	GameClosed(name string)
	CloseConnection()
}

type ServerCallback interface {
	CreateGame(name string, password string) bool
}
