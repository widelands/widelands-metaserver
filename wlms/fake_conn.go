package main

import (
	"io"
	. "gopkg.in/check.v1"
	"github.com/widelands/widelands_metaserver/wlms/packet"
	"net"
)

type FakeAddr struct{}

func (a FakeAddr) Network() string {
	return "TestingNetwork"
}
func (a FakeAddr) String() string {
	return "192.168.0.0:1234"
}

type FakeConn struct {
	Packets         chan *packet.Packet
	sendData_Reader *io.PipeReader
	sendData_Writer *io.PipeWriter
	recvData_Reader *io.PipeReader
	recvData_Writer *io.PipeWriter

	gotClosed *bool
}

func NewFakeConn(c *C) FakeConn {
	f := FakeConn{Packets: make(chan *packet.Packet, 20), gotClosed: new(bool)}
	f.sendData_Reader, f.sendData_Writer = io.Pipe()
	f.recvData_Reader, f.recvData_Writer = io.Pipe()
	go f.readPackets()
	return f
}

func (f FakeConn) readPackets() {
	for {
		pkg, err := packet.Read(f.ServerReader())
		if err != nil {
			break
		}
		f.Packets <- pkg
	}
}

func (f FakeConn) ServerWriter() io.Writer {
	return f.sendData_Writer
}

func (f FakeConn) ServerReader() io.Reader {
	return f.recvData_Reader
}

func (f FakeConn) GotClosed() bool {
	return *f.gotClosed
}

func (f FakeConn) Read(b []byte) (int, error) {
	n, err := f.sendData_Reader.Read(b)
	return n, err
}

func (f FakeConn) Write(b []byte) (n int, err error) {
	return f.recvData_Writer.Write(b)
}

func (f FakeConn) Close() error {
	f.sendData_Reader.Close()
	f.sendData_Writer.Close()
	f.recvData_Reader.Close()
	f.recvData_Writer.Close()
	*f.gotClosed = true
	return nil
}

func (f FakeConn) RemoteAddr() net.Addr {
	return FakeAddr{}
}
