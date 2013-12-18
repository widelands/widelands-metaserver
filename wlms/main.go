package main

func main() {
	rc := CreateRemoteControl()
	listener := CreateListener()

	for done := false; !done; {
		select {
		case <-rc.Quit:
			listener.Quit <- true // TODO(sirver): this value is not really handled
			done = true
		}
	}
	println("Exiting!")
}
