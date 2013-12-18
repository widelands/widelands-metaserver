package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

type ReaderWithDeadline interface {
	io.Reader
	SetDeadline(time.Time) error
}

type Packet struct {
	Content []string
}

type Connection struct {
	c        ReaderWithDeadline
	Packages chan Packet
}

func NewConnection(r ReaderWithDeadline) *Connection {
	return &Connection{r, make(chan Packet)}
}

func (c *Connection) readInt() (int, error) {
	buf := make([]byte, 2)

	c.c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(c.c, buf); err != nil {
		return 0, err
	}
	return (int(buf[0]) << 8) + int(buf[1]), nil
}

func (c *Connection) readString(nlen int) (string, error) {
	buf := make([]byte, nlen)
	if _, err := io.ReadFull(c.c, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func (c *Connection) readPacket() ([]string, error) {
	nlen, err := c.readInt()
	if err != nil {
		return []string{}, err
	}
	fmt.Printf("nlen: %v\n", nlen)
	str, err := c.readString(nlen - 2)
	if err != nil {
		return []string{}, err
	}
	fmt.Printf("str: %v\n", str)

	return strings.Split(str, "\x00"), nil
}

type Listener struct {
	Quit chan bool

	l net.Listener
}

func handleConnection(conn net.Conn) {
	c := NewConnection(conn)

	err, pkg := c.readPacket()
	if err != nil {
		// TODO(sirver): do something
		fmt.Printf("err: %v\n", err)
	}
	fmt.Printf("pkg: %v\n", pkg)
}

func (l *Listener) connectLoop() {
	for {
		conn, err := l.l.Accept()
		if err != nil {
			// TODO(sirver): handle error
			continue
		}
		go handleConnection(conn)
	}
}

func CreateListener() *Listener {
	ln, err := net.Listen("tcp", ":7395") // TODO(sirver): softcode this
	if err != nil {
		log.Fatal(err)
	}
	rv := &Listener{make(chan bool), ln}
	rv.connectLoop()
	return rv
}
