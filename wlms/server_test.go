package main

import (
	. "launchpad.net/gocheck"
	"launchpad.net/widelands-metaserver/wlms/packet"
	"log"
	"testing"
	"time"
)

// Hook up gocheck into the gotest runner.
func Test(t *testing.T) { TestingT(t) }

func WriteDataToConnection(conn FakeConn, data ...string) {
	go func() {
		for _, d := range data {
			conn.Write([]byte(d))
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func SendPacket(f FakeConn, data ...interface{}) {
	f.ServerWriter().Write(packet.New(data...))
}

func ExpectClosed(c *C, f FakeConn) {
	c.Assert(f.GotClosed(), Equals, true)
}

func SetupServer(c *C, nClients int) (*Server, []FakeConn) {
	log.SetFlags(log.Lshortfile)
	db := NewInMemoryDb()
	db.AddUser("SirVer", "123456", SUPERUSER)
	db.AddUser("otto", "ottoiscool", REGISTERED)

	acceptingConnections := make(chan ReadWriteCloserWithIp, 20)
	cons := make([]FakeConn, nClients)
	for i := range cons {
		cons[i] = NewFakeConn(c)
		acceptingConnections <- cons[i]
	}

	//irc := NewIRCBridge("irc.freenode.net:7000", "wltest", "wltest", "#widelands-test", true)
	toIrc := make(chan Message, 100)
	fromIrc := make(chan Message, 100)
	//irc.Connect(toIrc, fromIrc)
	return CreateServerUsing(acceptingConnections, db, fromIrc, toIrc), cons
}

type Matching string

func ExpectPacket(c *C, f FakeConn, expected ...interface{}) {
	timer := time.NewTimer(20 * time.Millisecond)
	select {
	case packet := <-f.Packets:
		c.Check(len(packet.RawData), Equals, len(expected))
		for i := 0; i < len(packet.RawData); i += 1 {
			switch expect := expected[i].(type) {
			case Matching:
				c.Check(packet.RawData[i], Matches, string(expect))
				continue
			default:
				c.Check(packet.RawData[i], Equals, expected[i])
			}
		}
	case <-timer.C:
		c.Errorf("No packet arrived, though we expected one.")
	}
}

func ExpectPacketForAll(c *C, clients []FakeConn, expected ...interface{}) {
	for _, client := range clients {
		ExpectPacket(c, client, expected...)
	}
}

func ExpectLoginAsUnregisteredWorks(c *C, f FakeConn, name string) {
	SendPacket(f, "LOGIN", 0, name, "build-16", false)
	ExpectPacket(c, f, "LOGIN", name, "UNREGISTERED")
	ExpectPacket(c, f, "TIME", Matching("\\d+"))
	ExpectPacket(c, f, "CLIENTS_UPDATE")
}

func ExpectLoginAsOttoWorks(c *C, f FakeConn) {
	SendPacket(f, "LOGIN", 0, "otto", "build-17", true, "ottoiscool")
	ExpectPacket(c, f, "LOGIN", "otto", "REGISTERED")
	ExpectPacket(c, f, "TIME", Matching("\\d+"))
	ExpectPacket(c, f, "CLIENTS_UPDATE")
}

func ExpectLoginAsSirVerWorks(c *C, f FakeConn) {
	SendPacket(f, "LOGIN", 0, "SirVer", "build-18", true, "123456")
	ExpectPacket(c, f, "LOGIN", "SirVer", "SUPERUSER")
	ExpectPacket(c, f, "TIME", Matching("\\d+"))
	ExpectPacket(c, f, "CLIENTS_UPDATE")
}

func ExpectServerToShutdownCleanly(c *C, server *Server) {
	server.InitiateShutdown()
	server.WaitTillShutdown()
	c.Assert(server.NrActiveClients(), Equals, 0)
}

type EndToEndSuite struct{}

var _ = Suite(&EndToEndSuite{})

// Test Packet decoding {{{
func (s *EndToEndSuite) TestSimplePacket(c *C) {
	conn := NewFakeConn(c)
	WriteDataToConnection(conn, "\x00\x07aaaa\x00")
	ExpectPacket(c, conn, "aaaa")
}

func (s *EndToEndSuite) TestSimplePacket1(c *C) {
	conn := NewFakeConn(c)
	WriteDataToConnection(conn, "\x00\x10aaaa\x00bbb\x00cc\x00d\x00")
	ExpectPacket(c, conn, "aaaa", "bbb", "cc", "d")
}

func (s *EndToEndSuite) TestTwoPacketsInOneRead(c *C) {
	conn := NewFakeConn(c)
	WriteDataToConnection(conn, "\x00\x07aaaa\x00\x00\x07aaaa\x00")
	ExpectPacket(c, conn, "aaaa")
	ExpectPacket(c, conn, "aaaa")
}

func (p *EndToEndSuite) TestFragmentedPackets(c *C) {
	conn := NewFakeConn(c)
	WriteDataToConnection(conn, "\x00\x0aCLI", "ENTS\x00\x00\x0a", "CLIENTS\x00\x00\x08")
	ExpectPacket(c, conn, "CLIENTS")
	ExpectPacket(c, conn, "CLIENTS")
}

// }}}
// Test Login {{{s
func (s *EndToEndSuite) TestRegisteredUserIncorrectPassword(c *C) {
	server, clients := SetupServer(c, 2)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "build-18", true, "23456")
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "WRONG_PASSWORD")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserNotExisting(c *C) {
	server, clients := SetupServer(c, 2)

	SendPacket(clients[0], "LOGIN", 0, "bluba", "build-16", true, "123456")
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "WRONG_PASSWORD")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginAnonymouslyWorks(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 0, "testuser", "build-16", false)

	ExpectPacket(c, clients[0], "LOGIN", "testuser", "UNREGISTERED")
	ExpectPacket(c, clients[0], "TIME", Matching("\\d+"))
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	clients[0].Close()

	time.Sleep(5 * time.Millisecond)
	c.Assert(server.NrActiveClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
	ExpectClosed(c, clients[0])
}

func (s *EndToEndSuite) TestLoginUnknownProtocol(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 10, "testuser", "build-16", false)
	ExpectPacket(c, clients[0], "ERROR", "LOGIN", "UNSUPPORTED_PROTOCOL")

	time.Sleep(5 * time.Millisecond)
	c.Assert(server.NrActiveClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginWithKnownUserName(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "build-18", false)
	ExpectPacket(c, clients[0], "LOGIN", "SirVer1", "UNREGISTERED")
	ExpectPacket(c, clients[0], "TIME", Matching("\\d+"))
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestLoginOneWasAlreadyThere(c *C) {
	server, clients := SetupServer(c, 2)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "testuser")

	SendPacket(clients[1], "LOGIN", 0, "testuser", "build-16", false)
	ExpectPacket(c, clients[1], "LOGIN", "testuser1", "UNREGISTERED")
	ExpectPacket(c, clients[1], "TIME", Matching("\\d+"))
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserCorrectPassword(c *C) {
	server, clients := SetupServer(c, 2)

	SendPacket(clients[0], "LOGIN", 0, "SirVer", "build-18", true, "123456")
	ExpectPacket(c, clients[0], "LOGIN", "SirVer", "SUPERUSER")
	ExpectPacket(c, clients[0], "TIME", Matching("\\d+"))
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	SendPacket(clients[1], "LOGIN", 0, "otto", "build-17", true, "ottoiscool")
	ExpectPacket(c, clients[1], "LOGIN", "otto", "REGISTERED")
	ExpectPacket(c, clients[1], "TIME", Matching("\\d+"))
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestRegisteredUserAlreadyLoggedIn(c *C) {
	server, clients := SetupServer(c, 2)

	ExpectLoginAsSirVerWorks(c, clients[0])

	SendPacket(clients[1], "LOGIN", 0, "SirVer", "build-18", true, "123456")
	ExpectPacket(c, clients[1], "ERROR", "LOGIN", "ALREADY_LOGGED_IN")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}

// Test Relogin {{{
func (e *EndToEndSuite) TestReloginPingAndReply(c *C) {
	server, clients := SetupServer(c, 2)
	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")

	SendPacket(clients[1], "RELOGIN", 0, "bert", "build-16", false)

	ExpectPacket(c, clients[0], "PING")
	SendPacket(clients[0], "PONG")

	ExpectPacket(c, clients[1], "ERROR", "RELOGIN", "CONNECTION_STILL_ALIVE")
	ExpectClosed(c, clients[1])

	c.Assert(server.NrActiveClients(), Equals, 1)
	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestReloginForNonAnonymous(c *C) {
	server, clients := SetupServer(c, 2)
	ExpectLoginAsOttoWorks(c, clients[0])

	server.SetPingCycleTime(5 * time.Millisecond)

	SendPacket(clients[1], "RELOGIN", 0, "otto", "build-17", true, "ottoiscool")
	ExpectPacket(c, clients[0], "PING")

	time.Sleep(6 * time.Millisecond)

	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	ExpectClosed(c, clients[0])

	ExpectPacket(c, clients[1], "RELOGIN")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	c.Assert(server.NrActiveClients(), Equals, 1)
	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestReloginPingAndNoReply(c *C) {
	server, clients := SetupServer(c, 3)
	server.SetPingCycleTime(5 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
	ExpectLoginAsUnregisteredWorks(c, clients[2], "ernie")
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	SendPacket(clients[1], "RELOGIN", 0, "bert", "build-16", false)

	time.Sleep(2 * time.Millisecond)
	ExpectPacket(c, clients[0], "PING")

	SendPacket(clients[2], "PONG")
	time.Sleep(6 * time.Millisecond) // No reply for client 0

	// Connection terminated for old user.
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	ExpectClosed(c, clients[0])

	ExpectPacket(c, clients[1], "RELOGIN")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	ExpectPacket(c, clients[2], "CLIENTS_UPDATE")

	c.Assert(server.NrActiveClients(), Equals, 2)
	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestReloginNotLoggedIn(c *C) {
	server, clients := SetupServer(c, 1)

	SendPacket(clients[0], "RELOGIN", 0, "bert", "build-16", false)
	ExpectPacket(c, clients[0], "ERROR", "RELOGIN", "NOT_LOGGED_IN")
	ExpectClosed(c, clients[0])

	c.Assert(server.NrActiveClients(), Equals, 0)
	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestReloginWrongInformations(c *C) {
	server, clients := SetupServer(c, 10)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
	ExpectLoginAsSirVerWorks(c, clients[1])

	// Wrong Proto
	SendPacket(clients[2], "RELOGIN", 1, "bert", "build-16", false)
	ExpectPacket(c, clients[2], "ERROR", "RELOGIN", "WRONG_INFORMATION")
	ExpectClosed(c, clients[2])

	// Wrong buildid.
	SendPacket(clients[3], "RELOGIN", 0, "bert", "build-10", false)
	ExpectPacket(c, clients[3], "ERROR", "RELOGIN", "WRONG_INFORMATION")
	ExpectClosed(c, clients[3])

	// Claim we are registered
	SendPacket(clients[4], "RELOGIN", 0, "bert", "build-16", true, "123123")
	ExpectPacket(c, clients[4], "ERROR", "RELOGIN", "WRONG_INFORMATION")
	ExpectClosed(c, clients[4])

	// Claim we are unregistered.
	SendPacket(clients[5], "RELOGIN", 0, "SirVer", "build-18", false)
	ExpectPacket(c, clients[5], "ERROR", "RELOGIN", "WRONG_INFORMATION")
	ExpectClosed(c, clients[5])

	// Wrong password.
	SendPacket(clients[6], "RELOGIN", 0, "SirVer", "build-18", true, "13245")
	ExpectPacket(c, clients[6], "ERROR", "RELOGIN", "WRONG_INFORMATION")
	ExpectClosed(c, clients[6])

	c.Assert(server.NrActiveClients(), Equals, 2)
	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestRelogWhileInGame(c *C) {
	server, clients, _ := gameTestSetup(c, false)

	server.SetPingCycleTime(20 * time.Millisecond)
	server.SetGamePingTimeout(5 * time.Second) // Not interesting in this.

	ExpectForTwo := func(data ...interface{}) {
		for i := 0; i < 2; i++ {
			ExpectPacket(c, clients[i], data...)
		}
	}

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectForTwo("GAMES_UPDATE")
	ExpectForTwo("CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "GAME_CONNECT", "192.168.0.0")
	ExpectForTwo("CLIENTS_UPDATE")

	// Syncronize ping timers.
	SendPacket(clients[0], "PONG")
	SendPacket(clients[1], "PONG")
	time.Sleep(6 * time.Millisecond)
	ExpectForTwo("PING")

	SendPacket(clients[1], "PONG") // Only reply for one user.
	time.Sleep(3 * time.Millisecond)
	SendPacket(clients[1], "PONG") // Only reply for one user.
	time.Sleep(3 * time.Millisecond)

	// Connection was terminated for old user
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")

	// Make sure we see the one player getting disconnected.
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[1], "PING")
	SendPacket(clients[1], "PONG")

	// Relogin.
	SendPacket(clients[2], "RELOGIN", 0, "bert", "build-16", false)
	ExpectPacket(c, clients[2], "RELOGIN")
	ExpectPacket(c, clients[2], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "2",
		"otto", "build-17", "my cool game", "REGISTERED", "",
		"bert", "build-16", "my cool game", "UNREGISTERED", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (e *EndToEndSuite) TestRelogWhileInGameRealWorldExample(c *C) {
	server, clients, pinger := gameTestSetup(c, false)

	server.SetPingCycleTime(20 * time.Millisecond)
	server.SetGamePingTimeout(10 * time.Millisecond)

	// The game never replies.
	for i := 0; i < 50; i++ {
		pinger.C <- false
	}

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients[:2], "GAMES_UPDATE")
	ExpectPacketForAll(c, clients[:2], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[0], "ERROR", "GAME_OPEN", "GAME_TIMEOUT")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "GAME_CONNECT", "192.168.0.0")
	ExpectPacketForAll(c, clients[:2], "GAMES_UPDATE")
	ExpectPacketForAll(c, clients[:2], "CLIENTS_UPDATE")

	SendPacket(clients[0], "GAME_START")
	ExpectPacket(c, clients[0], "GAME_START")
	ExpectPacketForAll(c, clients[:2], "GAMES_UPDATE")

	// Syncronize ping timers.
	SendPacket(clients[0], "PONG")
	SendPacket(clients[1], "PONG")
	time.Sleep(6 * time.Millisecond)
	ExpectPacketForAll(c, clients[:2], "PING")

	// No reply for host.
	SendPacket(clients[1], "PONG")
	time.Sleep(21 * time.Millisecond)

	// Make sure we see the one player getting disconnected.
	ExpectPacket(c, clients[0], "DISCONNECT", "CLIENT_TIMEOUT")
	ExpectPacket(c, clients[1], "PING")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
	SendPacket(clients[1], "PONG")
	ExpectClosed(c, clients[0])

	// Relogin.
	SendPacket(clients[2], "RELOGIN", 0, "bert", "build-16", false)
	ExpectPacket(c, clients[2], "RELOGIN")
	ExpectPacketForAll(c, clients[1:], "CLIENTS_UPDATE")

	// Syncronize ping timers.
	SendPacket(clients[1], "PONG")
	SendPacket(clients[2], "PONG")
	time.Sleep(6 * time.Millisecond)
	ExpectPacketForAll(c, clients[1:], "PING")

	// No reply for host (again).
	SendPacket(clients[1], "PONG")
	time.Sleep(21 * time.Millisecond)

	// Make sure we see the one player getting disconnected.
	ExpectPacket(c, clients[2], "DISCONNECT", "CLIENT_TIMEOUT")

	ExpectPacket(c, clients[1], "PING")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
	SendPacket(clients[1], "PONG")
	ExpectClosed(c, clients[2])

	// Sanity check: only one client on the server?
	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "1",
		"otto", "build-17", "my cool game", "REGISTERED", "")

	// Let's ping a few times.
	time.Sleep(21 * time.Millisecond)
	ExpectPacket(c, clients[1], "PING")
	SendPacket(clients[1], "PONG")

	// By now the clients should be forgotten and the game deleted.
	ExpectPacket(c, clients[1], "GAMES_UPDATE")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	time.Sleep(21 * time.Millisecond)
	ExpectPacket(c, clients[1], "PING")
	SendPacket(clients[1], "PONG")

	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "1",
		"otto", "build-17", "my cool game", "REGISTERED", "")

	SendPacket(clients[1], "GAMES")
	ExpectPacket(c, clients[1], "GAMES", "0")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Disconnect {{{
func (e *EndToEndSuite) TestDisconnect(c *C) {
	server, clients := SetupServer(c, 2)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
	ExpectLoginAsOttoWorks(c, clients[1])

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	SendPacket(clients[0], "DISCONNECT", "Gotta fly now!")
	clients[0].Close()

	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	c.Assert(server.NrActiveClients(), Equals, 1)
	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Chat {{{
func (e *EndToEndSuite) TestChat(c *C) {
	server, clients := SetupServer(c, 2)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
	ExpectLoginAsUnregisteredWorks(c, clients[1], "ernie")

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	// Send public messages.
	SendPacket(clients[0], "CHAT", "hello there", "")
	ExpectPacket(c, clients[0], "CHAT", "bert", "hello there", "public")
	ExpectPacket(c, clients[1], "CHAT", "bert", "hello there", "public")
	SendPacket(clients[0], "CHAT", "hello <rt>there</rt>\nhow<rtdoyoudo", "")
	ExpectPacket(c, clients[0], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "public")
	ExpectPacket(c, clients[1], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "public")

	// Send private messages.
	SendPacket(clients[0], "CHAT", "hello there", "ernie")
	SendPacket(clients[0], "CHAT", "hello <rt>there</rt>\nhow<rtdoyoudo", "ernie")
	ExpectPacket(c, clients[1], "CHAT", "bert", "hello there", "private")
	ExpectPacket(c, clients[1], "CHAT", "bert", "hello &lt;rt>there&lt;/rt>\nhow&lt;rtdoyoudo", "private")

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
	c.Assert(server.NrActiveClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Pinging {{{
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

	c.Assert(server.NrActiveClients(), Equals, 0)

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Motd {{{
func (s *EndToEndSuite) TestMotd(c *C) {
	server, clients := SetupServer(c, 3)

	ExpectLoginAsSirVerWorks(c, clients[0])
	ExpectLoginAsOttoWorks(c, clients[1])
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	// Check Superuser setting motd.
	SendPacket(clients[0], "MOTD", "Schnulz is cool!")
	ExpectPacket(c, clients[0], "CHAT", "", "Schnulz is cool!", "system")
	ExpectPacket(c, clients[1], "CHAT", "", "Schnulz is cool!", "system")

	// Check normal user setting motd.
	SendPacket(clients[1], "MOTD", "Schnulz is cool!")
	ExpectPacket(c, clients[1], "ERROR", "MOTD", "DEFICIENT_PERMISSION")
	// This will not close your connection.

	// Login and you'll receive a motd.
	ExpectLoginAsUnregisteredWorks(c, clients[2], "bert")
	ExpectPacket(c, clients[2], "CHAT", "", "Schnulz is cool!", "system")

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Announcement  {{{
func (s *EndToEndSuite) TestAnnouncement(c *C) {
	server, clients := SetupServer(c, 3)

	ExpectLoginAsSirVerWorks(c, clients[0])
	ExpectLoginAsOttoWorks(c, clients[1])
	ExpectLoginAsUnregisteredWorks(c, clients[2], "bert")
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
	ExpectPacket(c, clients[1], "CLIENTS_UPDATE")

	// Check Superuser announcing.
	SendPacket(clients[0], "ANNOUNCEMENT", "Schnulz is cool!")
	ExpectPacket(c, clients[1], "CHAT", "", "Schnulz is cool!", "system")
	ExpectPacket(c, clients[2], "CHAT", "", "Schnulz is cool!", "system")

	// Check other users can not announce.
	SendPacket(clients[1], "ANNOUNCEMENT", "Schnulz is cool!")
	SendPacket(clients[2], "ANNOUNCEMENT", "Schnulz is cool!")
	ExpectPacket(c, clients[1], "ERROR", "ANNOUNCEMENT", "DEFICIENT_PERMISSION")
	ExpectPacket(c, clients[2], "ERROR", "ANNOUNCEMENT", "DEFICIENT_PERMISSION")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Game interaction {{{
type FakeGamePingerFactory struct {
	pinger GamePinger
}

func (f FakeGamePingerFactory) New(client *Client, timeout time.Duration) *GamePinger {
	return &f.pinger
}

func gameTestSetup(c *C, loginThirdConnection bool) (*Server, []FakeConn, GamePinger) {
	server, clients := SetupServer(c, 3)
	server.SetClientForgetTimeout(30 * time.Millisecond)

	pinger := GamePinger{
		make(chan bool, 100),
	}
	server.InjectGamePingCreator(FakeGamePingerFactory{pinger})
	server.SetGamePingTimeout(5 * time.Millisecond)

	ExpectLoginAsUnregisteredWorks(c, clients[0], "bert")
	ExpectLoginAsOttoWorks(c, clients[1])

	ExpectPacket(c, clients[0], "CLIENTS_UPDATE")

	if loginThirdConnection {
		ExpectLoginAsSirVerWorks(c, clients[2])
		ExpectPacket(c, clients[0], "CLIENTS_UPDATE")
		ExpectPacket(c, clients[1], "CLIENTS_UPDATE")
	}

	return server, clients, pinger
}

func (s *EndToEndSuite) TestCreateGameAndPingReply(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	pinger.C <- true

	ExpectPacket(c, clients[0], "GAME_OPEN")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestCreateGameAndNoConnection(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	pinger.C <- false

	// No reply to game ping.
	time.Sleep(6 * time.Millisecond)

	ExpectPacket(c, clients[0], "ERROR", "GAME_OPEN", "GAME_TIMEOUT")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	SendPacket(clients[2], "GAMES")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")
	ExpectPacket(c, clients[2], "GAMES", "1",
		"my cool game", "build-16", "false")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestCreateGameTwicePrePing(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_OPEN", "my cool game", 12)
	ExpectPacket(c, clients[1], "ERROR", "GAME_OPEN", "GAME_EXISTS")

	pinger.C <- true

	ExpectPacket(c, clients[0], "GAME_OPEN")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestCreateGameTwicePostPing(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	pinger.C <- true

	ExpectPacket(c, clients[0], "GAME_OPEN")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[1], "GAME_OPEN", "my cool game", 12)
	ExpectPacket(c, clients[1], "ERROR", "GAME_OPEN", "GAME_EXISTS")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestCreateGameAndNoFirstPingReply(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	time.Sleep(6 * time.Millisecond)
	pinger.C <- false

	ExpectPacket(c, clients[0], "ERROR", "GAME_OPEN", "GAME_TIMEOUT")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	SendPacket(clients[2], "GAMES")
	ExpectPacket(c, clients[2], "GAMES", "1",
		"my cool game", "build-16", "false")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestCreateGameAndNoSecondPingReply(c *C) {
	server, clients, pinger := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	pinger.C <- true

	ExpectPacket(c, clients[0], "GAME_OPEN")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	pinger.C <- false

	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	SendPacket(clients[2], "GAMES")
	ExpectPacket(c, clients[2], "GAMES", "0")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestJoinGame(c *C) {
	server, clients, _ := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "GAME_CONNECT", "192.168.0.0")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "my cool game", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestJoinFullGame(c *C) {
	server, clients, _ := gameTestSetup(c, true)

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 1)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "ERROR", "GAME_CONNECT", "GAME_FULL")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestJoinNonexistingGame(c *C) {
	server, clients, _ := gameTestSetup(c, true)

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "ERROR", "GAME_CONNECT", "NO_SUCH_GAME")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
// Test Game Starting {{{
func (s *EndToEndSuite) TestStartGame(c *C) {
	server, clients, _ := gameTestSetup(c, true)

	server.SetGamePingTimeout(5 * time.Second) // Not interested in this.

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "GAME_CONNECT", "192.168.0.0")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	// Try to start a game without being in one.
	SendPacket(clients[2], "GAME_START")
	ExpectPacket(c, clients[2], "ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
	ExpectClosed(c, clients[2])
	clients = clients[:2]
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	// Try starting without being a host.
	SendPacket(clients[1], "GAME_START")
	ExpectPacket(c, clients[1], "ERROR", "GAME_START", "DEFICIENT_PERMISSION")

	// And a successful start.
	SendPacket(clients[0], "GAME_START")
	ExpectPacket(c, clients[0], "GAME_START")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}

// Test Game Leaving {{{
func gameLeavingSetup(c *C, loginThirdConnection bool) (*Server, []FakeConn) {
	server, clients, _ := gameTestSetup(c, true)

	server.SetGamePingTimeout(5 * time.Second) // Not interested in this.

	SendPacket(clients[0], "GAME_OPEN", "my cool game", 8)
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[1], "GAME_CONNECT", "my cool game")
	ExpectPacket(c, clients[1], "GAME_CONNECT", "192.168.0.0")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	return server, clients
}

func (s *EndToEndSuite) TestGameNonHostLeaving(c *C) {
	server, clients := gameLeavingSetup(c, true)

	SendPacket(clients[1], "GAME_DISCONNECT")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestGameHostLeaving(c *C) {
	server, clients := gameLeavingSetup(c, true)

	SendPacket(clients[0], "GAME_DISCONNECT")
	ExpectPacketForAll(c, clients, "GAMES_UPDATE")
	ExpectPacketForAll(c, clients, "CLIENTS_UPDATE")

	SendPacket(clients[2], "CLIENTS")
	ExpectPacket(c, clients[2], "CLIENTS", "3",
		"bert", "build-16", "", "UNREGISTERED", "",
		"otto", "build-17", "my cool game", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

func (s *EndToEndSuite) TestGameLeavingNotInGame(c *C) {
	server, clients := gameLeavingSetup(c, true)

	SendPacket(clients[2], "GAME_DISCONNECT")

	SendPacket(clients[1], "CLIENTS")
	ExpectPacket(c, clients[1], "CLIENTS", "3",
		"bert", "build-16", "my cool game", "UNREGISTERED", "",
		"otto", "build-17", "my cool game", "REGISTERED", "",
		"SirVer", "build-18", "", "SUPERUSER", "")

	ExpectServerToShutdownCleanly(c, server)
}

// }}}
