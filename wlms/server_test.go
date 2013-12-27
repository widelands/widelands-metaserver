package main

import (
	. "launchpad.net/gocheck"
	"launchpad.net/wlmetaserver/wlms/test_utils"
	"log"
	"testing"
	"time"
)

type Matching struct {
	String string
}

// Hook up gocheck into the gotest runner.
func Test(t *testing.T) { TestingT(t) }

type EndToEndSuite struct{}

var _ = Suite(&EndToEndSuite{})

func SendPacket(f test_utils.FakeConn, data ...interface{}) {
	f.ServerWriter().Write(BuildPacket(data...))
}

func ExpectPacket(c *C, f test_utils.FakeConn, expected ...interface{}) {
	packet, err := ReadPacket(f.ServerReader())
	log.Printf("packet: %v\n", packet)
	c.Assert(err, Equals, nil)
	c.Check(len(packet.data), Equals, len(expected))
	for i := 0; i < len(packet.data); i += 1 {
		switch expected[i].(type) {
		case Matching:
			c.Check(packet.data[i], Matches,
				expected[i].(Matching).String)
			continue
		default:
			c.Check(packet.data[i], Equals, expected[i])
		}
	}
}

func ExpectClosed(c *C, f test_utils.FakeConn) {
	c.Assert(f.GotClosed(), Equals, true)
}

func SetupServer(c *C, nClients int) (*Server, []test_utils.FakeConn) {
	log.SetFlags(log.Lshortfile)
	cons := make([]test_utils.FakeConn, nClients)
	for i := range cons {
		cons[i] = test_utils.NewFakeConn(c)
	}
	return CreateServerUsing(test_utils.NewFakeListener(cons)), cons
}

func ExpectLoginAsUnregisteredWorks(c *C, f test_utils.FakeConn, name string) {
	SendPacket(f, "LOGIN", 0, name, "bzr1234[trunk]", false)
	ExpectPacket(c, f, "LOGIN", name, "UNREGISTERED")
	ExpectPacket(c, f, "TIME", Matching{"\\d+"})
}

func ExpectServerToShutdownCleanly(c *C, server *Server) {
	server.Shutdown()
	server.WaitTillShutdown()
	c.Assert(server.NrClients(), Equals, 0)
}

func (s *EndToEndSuite) TestConnectionAndDone(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "bzr1234[trunk]", false)

	ExpectPacket(c, clients[0], "LOGIN", "SirVer", "UNREGISTERED")
	ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
	clients[0].Close()

	time.Sleep(5 * time.Millisecond)
	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
	ExpectClosed(c, clients[0])
}

func (s *EndToEndSuite) TestClientCanTimeout(c *C) {
	server, clients := SetupServer(c, 1)

	server.SetClientSendingTimeout(1 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "SirVer")

	time.Sleep(5 * time.Millisecond)
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	time.Sleep(5 * time.Millisecond)

	ExpectClosed(c, clients[0])
	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegularPingCycle(c *C) {
	server, clients := SetupServer(c, 1)

	server.SetPingCycleTime(5 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "SirVer")

	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")
	SendPacket(clients[0], "PONG")
	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")

	time.Sleep(15 * time.Millisecond)
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	time.Sleep(1 * time.Millisecond)
	ExpectClosed(c, clients[0])

	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}
