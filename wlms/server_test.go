package main

import (
	"io"
	. "launchpad.net/gocheck"
	"log"
	"net"
	"testing"
	"time"
)

type Matching struct {
	String string
}

// Hook up gocheck into the gotest runner.
func Test(t *testing.T) { TestingT(t) }

func ExpectPacket(c *C, r io.Reader, expected ...interface{}) {
	packet, err := ReadPacket(r)
	c.Assert(err, Equals, nil)
	c.Check(len(packet), Equals, len(expected))
	for i := 0; i < len(packet); i += 1 {
		switch expected[i].(type) {
		case Matching:
			c.Check(packet[i], Matches,
				expected[i].(Matching).String)
			continue
		default:
			c.Check(packet[i], Equals, expected[i])
		}
	}
}

func ExpectNoMorePackets(c *C, r FakeConn) {
	c.Assert(r.GotClosed, Equals, true)
}

type EndToEndSuite struct{}

var _ = Suite(&EndToEndSuite{})

type FakeAddr struct{}

func (a FakeAddr) Network() string {
	return "TestingNetwork"
}
func (a FakeAddr) String() string {
	return "TestingString"
}

type FakeListener struct {
	connections []*FakeConn
}

func (l FakeListener) Accept() (c net.Conn, err error) {
	for idx, conn := range l.connections {
		if conn != nil {
			returnValue := conn
			l.connections[idx] = nil
			return *returnValue, nil
		}
	}
	return nil, io.EOF
}
func (l FakeListener) Close() error { return nil }
func (l FakeListener) Addr() net.Addr {
	return FakeAddr{}
}

type FakeConn struct {
	SendData_Reader *io.PipeReader
	SendData_Writer *io.PipeWriter
	RecvData_Reader *io.PipeReader
	RecvData_Writer *io.PipeWriter

	GotClosed bool
}

func NewFakeConn() FakeConn {
	c := FakeConn{}
	c.SendData_Reader, c.SendData_Writer = io.Pipe()
	c.RecvData_Reader, c.RecvData_Writer = io.Pipe()
	return c
}

func (f FakeConn) Read(b []byte) (int, error) {
	n, err := f.SendData_Reader.Read(b)
	return n, err
}

func (f FakeConn) Write(b []byte) (n int, err error) {
	return f.RecvData_Writer.Write(b)
}

func (f FakeConn) Close() error {
	f.SendData_Reader.Close()
	f.SendData_Writer.Close()
	f.RecvData_Reader.Close()
	f.RecvData_Writer.Close()
	f.GotClosed = true
	return nil
}
func (f FakeConn) LocalAddr() net.Addr {
	return FakeAddr{}
}
func (f FakeConn) RemoteAddr() net.Addr {
	return FakeAddr{}
}
func (f FakeConn) SetDeadline(t time.Time) error {
	log.Print("Setting deadline %v", t)
	// NOCOM(sirver): implement
	return nil
}
func (f FakeConn) SetReadDeadline(t time.Time) error {
	// NOCOM(sirver): implement
	return nil
}
func (f FakeConn) SetWriteDeadline(t time.Time) error {
	// NOCOM(sirver): implement
	return nil
}

func (s *EndToEndSuite) TestConnectionAndDone(c *C) {
	con := NewFakeConn()
	listener := FakeListener{[]*FakeConn{&con}}
	server := CreateServerWithListener(CreateListenerUsing(listener))

	con.SendData_Writer.Write(BuildPacket("LOGIN", 0, "SirVer", "widelands", false))
	go server.runListeningLoop()

	ExpectPacket(c, con.RecvData_Reader, "LOGIN", "SirVer", "UNREGISTERED")
	ExpectPacket(c, con.RecvData_Reader, "TIME", Matching{"\\d+"})
	con.RecvData_Reader.Close()

	server.ShutdownServer <- true
	c.Assert(<-server.ServerHasShutdown, Equals, true)

	// NOCOM(sirver): this does not work properly.
	// ExpectNoMorePackets(c, con)
}
