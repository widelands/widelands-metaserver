package main

import (
	"bufio"
//	"bytes"
//	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
)
/*
type ReadWriteCloserWithIp interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}
*/
// Structure to bundle the TCP connection with its packet buffer
type Client struct {
	// The TCP connection to the client
	conn net.Conn

	// The id of this client when refering to him in messages to the host
	id uint8

	// Mutex to avoid two Sends at the same time
	mutex sync.Mutex

	// To read data from the network
	// I don't really like having a reader using a connection which is also
	// read from directly. But it seems to work
	reader *bufio.Reader

	//ReadUint8() uint8
//	ReadString() string
//	ReadPacket() []uint8
//	SendCommand(data ...interface{})
}

func New(c net.Conn) *Client {
	return &Client{
		conn: c,
		id: 0,
		reader: bufio.NewReader(c),
		}
}

func (c *Client) ReadUint8() (uint8, error) {
	b := make([]byte, 1)
	_, error := io.ReadFull(c.conn, b)
	return b[0], error
}

func (c *Client) ReadString() (string, error) {
log.Printf("readString 1\n")
	str, error := c.reader.ReadString('\000')
log.Printf("readString 2\n")
	// Remove final \0
	if len(str) > 0 {
		str = str[:len(str)-1]
	}
	return str, error
}

func (c *Client) ReadPacket() ([]byte, error) {
	length_bytes := make([]byte, 2)
	_, error := io.ReadFull(c.conn, length_bytes)
	if error != nil {
		return length_bytes, error
	}
	length := 0
	length = int(length_bytes[0]) << 8 | int(length_bytes[1])
	// Reduce length by 2 since the length-bytes are included in it
	//length -= 2
	//binary.Read(bytes.NewBuffer(length_bytes), binary.LittleEndian, &length)
//log.Printf("ReadPacket(): len[0]=%v, len[1]=%v, len=%v\n",length_bytes[0], length_bytes[1], length)
	packet := make([]byte, length)
	packet[0] = length_bytes[0]
	packet[1] = length_bytes[1]
	_, error = io.ReadFull(c.conn, packet[2:])
	// NOCOM: Think about this (and similar places). The client might be able to
	// keep the server waiting here. Actually, he can simply keep the connection
	// idling anyway. Is this a problem? Might be a possibility for DoS.
	// Is there a ping in the GameHost code? Won't help before a game is assigned
	// to the client, though. So probably add some fast disconnect on idle.
	return packet, error
}

func (c *Client) SendCommand(rawData ...interface{}) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, v := range rawData {
		switch v.(type) {
		case uint8:
			b := make([]byte, 1)
			b[0] = v.(uint8)
			c.conn.Write(b)
		case []byte:
			c.conn.Write(v.([]byte))
		case string:
			c.conn.Write([]byte(v.(string)))
			b := make([]byte, 1)
			b[0] = 0 // '\0'
			c.conn.Write(b)
		default:
			log.Fatal("Implementation error: Invalid data type in Client.SendCommand(), ignoring.")
		}
	}
}

// Sends a disconnect message and closes the connection
func (c *Client) Disconnect(reason string) {
	log.Printf("Disconnecting client because %v\n", reason)
	c.SendCommand(kDisconnect, reason)
	c.conn.Close()
}

