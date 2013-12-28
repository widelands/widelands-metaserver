package main

import (
	"io"
	"launchpad.net/wlmetaserver/wlms/packet"
	"log"
	"time"
)

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
	// The connection (net.Conn most likely) that let us talk to the other site.
	conn io.ReadWriteCloser

	// We always read one whole packet and send it over this to the consumer.
	DataStream chan *packet.Packet

	// the time when the user logged in for the first time. Relogins do not
	// update this time.
	loginTime time.Time

	// the protocol version used for communication
	protocolVersion int

	// the current connection state
	state State

	// is this a registered user/super user?
	permissions Permissions

	// name displayed in the GUI. This is guaranteed to be unique on the Server.
	userName string

	// the buildId of Widelands executable that this client is using.
	buildId string

	// Various state variables needed for fulfilling the protocol.
	startToPingTimer *time.Timer
	timeoutTimer     *time.Timer
	waitingForPong   bool
	pendingRelogin   *Client
}

func NewClient(r io.ReadWriteCloser) *Client {
	client := &Client{
		conn:        r,
		DataStream:  make(chan *packet.Packet, 10),
		state:       HANDSHAKE,
		permissions: UNREGISTERED,
	}
	go client.readingLoop()
	return client
}

func (c Client) ProtocolVersion() int {
	return c.protocolVersion
}
func (c *Client) SetProtocolVersion(v int) {
	c.protocolVersion = v
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
func (c *Client) SetBuildId(v string) {
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
	log.Printf("Sending to %s: %v\n", client.userName, data)
	client.conn.Write(packet.New(data...))
}

func (client *Client) readingLoop() {
	log.Print("Starting Goroutine: readingLoop")
	for {
		pkg, err := packet.Read(client.conn)
		if err != nil {
			log.Printf("Error reading packet: %v\n", err)
			break
		}
		client.DataStream <- pkg
	}
	client.Disconnect()
	log.Print("Ending Goroutine: readingLoop")
}

func (client Client) Name() string {
	return client.userName
}
func (client *Client) SetName(userName string) {
	client.userName = userName
}

func (client *Client) LoginTime() time.Time {
	return client.loginTime
}
func (client *Client) SetLoginTime(loginTime time.Time) {
	client.loginTime = loginTime
}
