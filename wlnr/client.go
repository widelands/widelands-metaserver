package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"time"
)

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
		pingTimer:       time.NewTimer(time.Second * 1),
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
	if length < 2 {
		return nil, io.ErrUnexpectedEOF
	}
	packet := make([]byte, length)
	packet[0] = length_bytes[0]
	packet[1] = length_bytes[1]
	_, error = io.ReadFull(c.reader, packet[2:])
	return packet, error
}

func (c *Client) SendCommand(cmd *Command) {
	c.chan_out <- cmd
}

// Sends a disconnect message and closes the connection
func (c *Client) Disconnect(reason string) {
	if c.conn == nil {
		return
	}
	log.Printf("Disconnecting client (id=%v) because %v\n", c.id, reason)
	cmd := NewCommand(kDisconnect)
	cmd.AppendString(reason)
	c.SendCommand(cmd)
	// Since conn.Close() indirectly calls this method again,
	// mark the connection as closed before calling it
	conn := c.conn
	c.conn = nil
	conn.Close()
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
			c.pingTimer.Reset(time.Second * 1)
		} else {
			// If it is over 90 seconds since the last received PONG,
			// the client is hanging for too long and we disconnect it.
			// Normally we should receive a PONG every second, but for build20
			// clients this might be delayed when loading maps
			if time.Now().After(c.timeLastPong.Add(time.Second * 90)) {
				// Bad luck: We got no response for too long so disconnect client
				// In the case of the game host this also takes down the game
				// by closing the socket -> game will notice it and abort
				log.Printf("Timeout of client (id=%v), disconnecting", c.id)
				c.Disconnect("TIMEOUT")
				break
			} else {
				// We are using TCP, so Pings can't get lost.
				// Just wait for some more time
				c.pingTimer.Reset(time.Second * 1)
			}
		}
	}
}

func (c *Client) HandlePong(seq uint8) {
	c.waitingForPong = false
	if seq != c.lastSendPingSeq {
		// Well, actually the sequence numbers are not that important,
		// as there should never be two pings open at the same time.
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
