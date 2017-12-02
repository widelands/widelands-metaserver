package main

import (
	"bufio"
	"io"
	"log"
	"net"
)

// Structure to bundle the TCP connection with its packet buffer
type Client struct {
	// The TCP connection to the client
	conn net.Conn

	// The id of this client when refering to him in messages to the host
	id uint8

	// To read data from the network
	reader *bufio.Reader

	// A channel for commands to send
	chan_out chan *Command
}

func New(conn net.Conn) *Client {
	client := &Client{
		conn:     conn,
		id:       0,
		reader:   bufio.NewReader(conn),
		chan_out: make(chan *Command),
	}
	go func() {
		for {
			cmd := <-client.chan_out
			client.conn.Write(cmd.GetBytes())
		}
	}()
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
	log.Printf("Disconnecting client because %v\n", reason)
	cmd := NewCommand(kDisconnect)
	cmd.AppendString(reason)
	c.SendCommand(cmd)
	c.conn.Close()
}
