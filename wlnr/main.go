package wlnr

func main() {
	server := StartServer()
	server.CreateGame("mygame", "pwd")
}
