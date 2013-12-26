package main

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"time"
)

// NOCOM(sirver): think about these again
const (
	anonymous_user = iota
	registered_user
)

const (
	just_connected = iota
	online
)

type Client struct {
	conn       net.Conn
	DataStream chan string

	name      string
	loginTime time.Time
}

func NewClient(r net.Conn) *Client {
	client := &Client{conn: r, DataStream: make(chan string)}
	go client.readingLoop()
	return client
}

func (client *Client) Disconnect() error {
	log.Printf("In Disconnect\n")
	client.conn.Close()
	if client.DataStream != nil {
		close(client.DataStream)
		client.DataStream = nil
	}
	return nil
}

func (client *Client) readingLoop() {
	log.Print("Starting Goroutine: readingLoop")
	for {
		pkg, err := ReadPacket(client.conn)
		if err != nil {
			// TODO(sirver): do something
			log.Printf("err: %v\n", err)
			break
		}
		for _, s := range pkg {
			client.DataStream <- s
		}
	}
	client.Disconnect()
	log.Print("Ending Goroutine: readingLoop")
}

func (client *Client) Name() string {
	return client.name
}
func (client *Client) SetName(name string) {
	client.name = name
}

func (client *Client) LoginTime() time.Time {
	return client.loginTime
}
func (client *Client) SetLoginTime(loginTime time.Time) {
	client.loginTime = loginTime
}

func (client *Client) ExpectInt() (int, error) {
	return strconv.Atoi(<-client.DataStream)
}

func (client *Client) ExpectBool() (bool, error) {
	d := <-client.DataStream
	switch d {
	case "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("Illegal argument for bool: %v.", d)
	}
}

func (client *Client) ExpectString() (string, error) {
	return <-client.DataStream, nil
}
