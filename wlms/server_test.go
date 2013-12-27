package main

import (
	. "launchpad.net/gocheck"
	"launchpad.net/wlmetaserver/wlms/test_utils"
	"log"
	"sync"
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
	db := NewInMemoryDb()
	db.AddUser("SirVer", "123456", SUPERUSER)
	db.AddUser("otto", "ottoiscool", REGISTERED)
	return CreateServerUsing(test_utils.NewFakeListener(cons), db), cons
}

func ExpectLoginAsUnregisteredWorks(c *C, f test_utils.FakeConn, name string) {
	SendPacket(f, "LOGIN", 0, name, "bzr1234[trunk]", false)
	ExpectPacket(c, f, "LOGIN", name, "UNREGISTERED")
	ExpectPacket(c, f, "TIME", Matching{"\\d+"})
	ExpectPacket(c, f, "CLIENTS_UPDATE")
}

func ExpectLoginAsRegisteredWorks(c *C, f test_utils.FakeConn, name, password string) {
	SendPacket(f, "LOGIN", 0, name, "bzr1234[trunk]", true, password)
	ExpectPacket(c, f, "LOGIN", name, Matching{"(REGISTERED|SUPERUSER)"})
	ExpectPacket(c, f, "TIME", Matching{"\\d+"})
	ExpectPacket(c, f, "CLIENTS_UPDATE")
}

func ExpectServerToShutdownCleanly(c *C, server *Server) {
	server.Shutdown()
	server.WaitTillShutdown()
	c.Assert(server.NrClients(), Equals, 0)
}

// Test Login {{{
func (s *EndToEndSuite) TestRegisteredUserIncorrectPassword(c *C) {
	server, clients := SetupServer(c, 2)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "bzr1234[trunk]", true, "23456")
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "WRONG_PASSWORD")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserNotExisting(c *C) {
	server, clients := SetupServer(c, 2)

	SendPacket(clients[0], "LOGIN", 0, "bluba", "bzr1234[trunk]", true, "123456")
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "WRONG_PASSWORD")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginAnonymouslyWorks(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 0, "testuser", "bzr1234[trunk]", false)

	ExpectPacket(c, clients[0], "LOGIN", "testuser", "UNREGISTERED")
	ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	clients[0].Close()

	time.Sleep(5 * time.Millisecond)
	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
	ExpectClosed(c, clients[0])
}

func (s *EndToEndSuite) TestLoginUnknownProtocol(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 10, "testuser", "bzr1234[trunk]", false)
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "UNSUPPORTED_PROTOCOL")

	time.Sleep(5 * time.Millisecond)
	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginWithKnownUserName(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "bzr1234[trunk]", false)
	ExpectPacket(c, clients[0], "LOGIN", "SirVer1", "UNREGISTERED")
	ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginOneWasAlreadyThere(c *C) {
	server, clients := SetupServer(c, 2)

	waitGrp := sync.WaitGroup{}
	SendPacket(clients[0], "LOGIN", 0, "testuser", "bzr1234[trunk]", false)
	waitGrp.Add(1)
	go func() {
		ExpectPacket(c, clients[0], "LOGIN", "testuser", "UNREGISTERED")
		ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()
	time.Sleep(1 * time.Millisecond)

	SendPacket(clients[1], "LOGIN", 0, "testuser", "bzr1234[trunk]", false)
	waitGrp.Add(1)
	go func() {
		ExpectPacket(c, clients[1], "LOGIN", "testuser1", "UNREGISTERED")
		ExpectPacket(c, clients[1], "TIME", Matching{"\\d+"})
		ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()

	waitGrp.Wait()

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserCorrectPassword(c *C) {
	server, clients := SetupServer(c, 2)

	waitGrp := sync.WaitGroup{}
	waitGrp.Add(2)
	go func() {
		SendPacket(clients[0], "LOGIN", 0, "SirVer", "bzr1234[trunk]", true, "123456")
		ExpectPacket(c, clients[0], "LOGIN", "SirVer", "SUPERUSER")
		ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()
	time.Sleep(5 * time.Millisecond)
	go func() {
		SendPacket(clients[1], "LOGIN", 0, "otto", "bzr1234[trunk]", true, "ottoiscool")
		ExpectPacket(c, clients[1], "LOGIN", "otto", "REGISTERED")
		ExpectPacket(c, clients[1], "TIME", Matching{"\\d+"})
		ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()

	waitGrp.Wait()
	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserAlreadyLoggedIn(c *C) {
	server, clients := SetupServer(c, 2)

	waitGrp := sync.WaitGroup{}
	waitGrp.Add(2)
	go func() {
		SendPacket(clients[0], "LOGIN", 0, "SirVer", "bzr1234[trunk]", true, "123456")
		ExpectPacket(c, clients[0], "LOGIN", "SirVer", "SUPERUSER")
		ExpectPacket(c, clients[0], "TIME", Matching{"\\d+"})
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()
	time.Sleep(5 * time.Millisecond)
	go func() {
		SendPacket(clients[1], "LOGIN", 0, "SirVer", "bzr1234[trunk]", true, "123456")
		ExpectPacket(c, clients[1], "ERROR", "LOGIN", "ALREADY_LOGGED_IN")
		waitGrp.Done()
	}()

	waitGrp.Wait()
	ExpectServerToShutdownCleanly(c, server)
}

/// }}}
// Test Disconnect {{{
func (e *EndToEndSuite) TestDisconnect(c *C) {
	server, clients := SetupServer(c, 2)

	waitGrp := sync.WaitGroup{}
	waitGrp.Add(2)
	go func() {
		ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		SendPacket(clients[0], "DISCONNECT", "Gotta fly now!")
		clients[0].Close()
		waitGrp.Done()
	}()
	time.Sleep(5 * time.Millisecond)

	go func() {
		ExpectLoginAsRegisteredWorks(c, clients[1], "otto", "ottoiscool")
		// this will arrive after the other client disconnects
		ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
		waitGrp.Done()
	}()

	waitGrp.Wait()

	c.Assert(server.NrClients(), Equals, 1)
	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Chat {{{
func (e *EndToEndSuite) TestChat(c *C) {
	server, clients := SetupServer(c, 2)

	waitGrp := sync.WaitGroup{}
	waitGrp.Add(2)
	go func() {
		ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

		SendPacket(clients[0], "CHAT", "hello there", "")
		ExpectPacket(c, clients[0], "CHAT", "bert", "hello there", "public")
		SendPacket(clients[0], "CHAT", "hello <rt>there</rt>\nhow<rtdoyoudo", "")
		ExpectPacket(c, clients[0], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "public")

		SendPacket(clients[0], "CHAT", "hello there", "ernie")
		SendPacket(clients[0], "CHAT", "hello <rt>there</rt>\nhow<rtdoyoudo", "ernie")

		waitGrp.Done()
	}()

	time.Sleep(5 * time.Millisecond)

	go func() {
		ExpectLoginAsUnregisteredWorks(c, clients[1], "ernie")
		ExpectPacket(c, clients[1], "CHAT", "bert", "hello there", "public")
		ExpectPacket(c, clients[1], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "public")
		ExpectPacket(c, clients[1], "CHAT", "bert", "hello there", "private")
		ExpectPacket(c, clients[1], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "private")
		waitGrp.Done()
	}()

	waitGrp.Wait()
	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Faulty Communication {{{
func (e *EndToEndSuite) TestUnknownPacket(c *C) {
	server, clients := SetupServer(c, 1)
	SendPacket(clients[0], "BLUMBAQUATSCH")
	ExpectPacket(c, clients[0], "ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")

	time.Sleep(5 * time.Millisecond)
	ExpectClosed(c, clients[0])

	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestWrongArgumentsInPacket(c *C) {
	server, clients := SetupServer(c, 1)
	SendPacket(clients[0], "LOGIN", "hi")
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "Invalid integer: 'hi'")

	time.Sleep(5 * time.Millisecond)
	ExpectClosed(c, clients[0])

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestClientCanTimeout(c *C) {
	server, clients := SetupServer(c, 1)

	server.SetClientSendingTimeout(1 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "testuser")

	time.Sleep(5 * time.Millisecond)
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	time.Sleep(5 * time.Millisecond)

	ExpectClosed(c, clients[0])
	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

// }}}

func (s *EndToEndSuite) TestRegularPingCycle(c *C) {
	server, clients := SetupServer(c, 1)

	server.SetPingCycleTime(5 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "testuser")

	// Regular Ping cycle
	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")
	SendPacket(clients[0], "PONG")
	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")

	// Regular packages are as good as a Pong.
	SendPacket(clients[0], "CHAT", "hello there", "")
	ExpectPacket(c, clients[0], "CHAT", "testuser", "hello there", "public")
	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")
	SendPacket(clients[0], "CHAT", "hello there", "")
	ExpectPacket(c, clients[0], "CHAT", "testuser", "hello there", "public")
	time.Sleep(6 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")

	// Timeout
	time.Sleep(15 * time.Millisecond)
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	time.Sleep(1 * time.Millisecond)
	ExpectClosed(c, clients[0])

	c.Assert(server.NrClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}
