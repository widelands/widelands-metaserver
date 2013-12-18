package main

import "launchpad.net/wlmetaserver/wlmslib"

func main() {
	rc := wlmslib.CreateRemoteControl()
	listener := wlmslib.CreateListener()

	for done := false; !done; {
		select {
		case <-rc.Quit:
			listener.Quit <- true // TODO(sirver): this value is not really handled
			done = true
		}
	}
	println("Exiting!")
}
