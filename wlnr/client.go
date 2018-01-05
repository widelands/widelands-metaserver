package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"time"
)

const PING_INTERVAL_S = 30

// Structure to bundle the TCP connection with its packet buffer
type Client struct {
	// The TCP connection to the client
	conn net.Conn

	// The id of this client when refering to him in messages to the host
	// This id is only unique inside one game
	id uint8

	// To read data from the network
	reader *bufio.Reader

	// A channel for commands to send
	chan_out chan *Command

	// A timer deciding when the next ping will be send
	pingTimer *time.Timer

	// Whether we are waiting for a Pong.
	// When its time to send a ping but we are already
	// waiting, the connection is probably lost.
	waitingForPong bool

	// The sequence number of the last send ping.
	// If waitingForPong is true, this is the number
	// we are waiting for
	lastSendPingSeq uint8

	// The time the last ping has been send
	// Needed to calculate the RTT of the ping
	timeLastPing time.Time

	// The time the last pong has been received
	timeLastPong time.Time

	// The time it took the last ping to be answered
	// Can't be calculated on the fly since timeLastPing might already
	// have been overwritten by the next ping
	rttLastPing time.Duration
}

func New(conn net.Conn) *Client {
	client := &Client{
		conn:            conn,
		id:              0,
		reader:          bufio.NewReader(conn),
		chan_out:        make(chan *Command),
		pingTimer:       time.NewTimer(time.Second * 1), // Do the next ping now
		waitingForPong:  false,
		lastSendPingSeq: 0,
		timeLastPing:    time.Now(),
		timeLastPong:    time.Now(),
		rttLastPing:     time.Since(time.Now()),
	}
	go func() {
		for {
			cmd := <-client.chan_out
			if client.conn == nil {
				break
			}
			client.conn.Write(cmd.GetBytes())
		}
	}()
	go client.pingLoop()
	return client
}

func (c *Client) ReadUint8() (uint8, error) {
	b := make([]byte, 1)
	_, error := io.ReadFull(c.reader, b)
	return b[0], error
}

func (c *Client) ReadString() (string, error) {
	str, error := c.reader.ReadString('\000')
	// Remove final \0
	if len(str) > 0 {
		str = str[:len(str)-1]
	}
	return str, error
}

func (c *Client) ReadPacket() ([]byte, error) {
	length_bytes := make([]byte, 2)
	_, error := io.ReadFull(c.reader, length_bytes)
	if error != nil {
		return length_bytes, error
	}
	length := int(length_bytes[0])<<8 | int(length_bytes[1])
	packet := make([]byte, length)
	packet[0] = length_bytes[0]
	packet[1] = length_bytes[1]
	_, error = io.ReadFull(c.reader, packet[2:])
	// TODO(Notabilis): Think about this (and similar places). The client might be able
	// to keep the server waiting here. Actually, he can simply keep the connection
	// idling anyway. Is this a problem? Might be a possibility for DoS.
	// Is there a ping in the GameHost code? Won't help before a game is assigned
	// to the client, though. So probably add some fast disconnect on idle.
	return packet, error
}

func (c *Client) SendCommand(cmd *Command) {
	c.chan_out <- cmd
}

// Sends a disconnect message and closes the connection
func (c *Client) Disconnect(reason string) {
	log.Printf("Disconnecting client (id=%v) because %v\n", c.id, reason)
	cmd := NewCommand(kDisconnect)
	cmd.AppendString(reason)
	c.SendCommand(cmd)
	c.conn.Close()
	c.conn = nil
}

func (c *Client) pingLoop() {
	for {
		<-c.pingTimer.C
		if c.conn == nil {
			// Seems we are disconnecting for some reason
			break
		}
		if c.waitingForPong == false {
			// Send the next ping
			c.waitingForPong = true
			c.timeLastPing = time.Now()
			c.lastSendPingSeq += 1
			cmd := NewCommand(kPing)
			cmd.AppendUInt(c.lastSendPingSeq)
			c.SendCommand(cmd)
			c.pingTimer.Reset(time.Second * PING_INTERVAL_S)
		} else {
			// Bad luck: We got no response so disconnect client
			// In the case of the game host this also takes down the game
			// by closing the socket -> game will notice it and abort
			log.Printf("Timeout of client (id=%v), disconnecting", c.id)
			c.Disconnect("TIMEOUT")
			break
		}
	}
}

func (c *Client) HandlePong(seq uint8) {
	c.waitingForPong = false
	if seq != c.lastSendPingSeq {
		// Well, actually the sequence numbers are not that important.
		// The client will be disconnected when he fails to respond in time,
		// so there should never be two pings open at the same time.
		// Could become important if we add, e.g., fast pings to check
		// whether a certain client is active.
		// Use the sequence number anyway so we don't mess up our
		// measurements when we get a pong too much
		return
	}
	c.timeLastPong = time.Now()
	c.rttLastPing = time.Since(c.timeLastPing)
}

func (c *Client) TimeLastPong() time.Time {
	return c.timeLastPong
}

func (c *Client) RttLastPing() time.Duration {
	return c.rttLastPing
}
