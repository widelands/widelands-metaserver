package main

func main() {
	s := CreateServer()

	s.WaitTillShutdown()
}
