package main

import (
	. "launchpad.net/gocheck"
	"launchpad.net/wlmetaserver/wlms/test_utils"
	"time"
)

type PacketSuite struct{}

var _ = Suite(&PacketSuite{})

func writeData(conn test_utils.FakeConn, data ...string) {
	go func() {
		for _, d := range data {
			conn.Write([]byte(d))
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func (s *PacketSuite) TestSimplePacket(c *C) {
	conn := test_utils.NewFakeConn(c)
	writeData(conn, "\x00\x07aaaa\x00")
	ExpectPacket(c, conn, "aaaa")
}

func (s *PacketSuite) TestSimplePacket1(c *C) {
	conn := test_utils.NewFakeConn(c)
	writeData(conn, "\x00\x10aaaa\x00bbb\x00cc\x00d\x00")
	ExpectPacket(c, conn, "aaaa", "bbb", "cc", "d")
}

func (s *PacketSuite) TestTwoPacketsInOneRead(c *C) {
	conn := test_utils.NewFakeConn(c)
	writeData(conn, "\x00\x07aaaa\x00\x00\x07aaaa\x00")
	ExpectPacket(c, conn, "aaaa")
	ExpectPacket(c, conn, "aaaa")
}

func (p *PacketSuite) TestFragmentedPackets(c *C) {
	conn := test_utils.NewFakeConn(c)
	writeData(conn, "\x00\x0aCLI", "ENTS\x00\x00\x0a", "CLIENTS\x00\x00\x08")
	ExpectPacket(c, conn, "CLIENTS")
	ExpectPacket(c, conn, "CLIENTS")
}
