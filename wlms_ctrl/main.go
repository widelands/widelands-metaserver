package main

import (
	"flag"
	"net"
	"os"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		println("Must specify exactly one argument!")
		os.Exit(-1)
	}

	c, err := net.Dial("unix", "/tmp/echo.sock")
	if err != nil {
		panic(err)
	}
	defer c.Close()

	println(flag.Arg(1))
	_, err = c.Write([]byte(flag.Arg(0)))
	if err != nil {
		println(err.Error())
	}
}
