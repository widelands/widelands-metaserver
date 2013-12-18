package main

import (
	"log"
	"net"
	"os"
)

type RemoteControl struct {
	Quit chan bool

	l net.Listener
}

func (rc *RemoteControl) handleCommand(cmd string) {
	switch cmd {
	case "quit":
		rc.Quit <- true
	default:
		println("Unknown command: ", cmd)
	}
}

func (rc *RemoteControl) echoServer(c net.Conn) {
	for {
		buf := make([]byte, 512)
		nr, err := c.Read(buf)
		if err != nil {
			return
		}

		cmdline := string(buf[0:nr])
		rc.handleCommand(cmdline)
		c.Close()
	}
}

func (rc *RemoteControl) echoAccept() {
	for {
		fd, err := rc.l.Accept()
		if err != nil {
			println("accept error", err.Error())
			return
		}

		go rc.echoServer(fd)
	}
}

func CreateRemoteControl() *RemoteControl {
	if err := os.Remove("/tmp/echo.sock"); err != nil && !os.IsNotExist(err) {
		log.Fatal("Could not open control socket: ", err.Error())
	} // TODO(sirver): rename and softcode

	l, err := net.Listen("unix", "/tmp/echo.sock")
	if err != nil {
		log.Fatal("listen error", err)
	}

	rc := &RemoteControl{make(chan bool), l}
	go rc.echoAccept()
	return rc
}
