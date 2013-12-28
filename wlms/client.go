package main

import (
	"launchpad.net/wlmetaserver/wlms/packet"
	"log"
	"net"
	"time"
)

// NOCOM(sirver): think about these again
type Permissions int

const (
	UNREGISTERED Permissions = iota
	REGISTERED
	SUPERUSER
)

func (p Permissions) String() string {
	switch p {
	case UNREGISTERED:
		return "UNREGISTERED"
	case REGISTERED:
		return "REGISTERED"
	case SUPERUSER:
		return "SUPERUSER"
	default:
		log.Fatalf("Unknown Permissions: %v", p)
	}
	// Never here
	return ""
}

type State int

const (
	HANDSHAKE State = iota
	CONNECTED
	DISCONNECTED
)

type Client struct {
	conn       net.Conn
	DataStream chan *packet.Packet

	state       State
	permissions Permissions
	name        string
	loginTime   time.Time
	buildId     string
}

func NewClient(r net.Conn) *Client {
	client := &Client{conn: r, DataStream: make(chan *packet.Packet, 10), state: HANDSHAKE, permissions: UNREGISTERED}
	go client.readingLoop()
	return client
}

func (c Client) Permissions() Permissions {
	return c.permissions
}
func (c *Client) SetPermissions(v Permissions) {
	c.permissions = v
}

func (c Client) State() State {
	return c.state
}
func (c *Client) SetState(s State) {
	c.state = s
}

func (c Client) BuildId() string {
	return c.buildId
}
func (c Client) SetBuildId(v string) {
	c.buildId = v
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

func (client *Client) SendPacket(data ...interface{}) {
	log.Printf("Sending to %s: %v\n", client.name, data)
	client.conn.Write(packet.New(data...))
}

func (client *Client) readingLoop() {
	log.Print("Starting Goroutine: readingLoop")
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			// TODO(sirver): do something
			log.Printf("err: %v\n", err)
			break
		}
		client.DataStream <- pkg
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
