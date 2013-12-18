package main

import (
	"bytes"
	"fmt"
	"io"
	. "launchpad.net/gocheck"
	"testing"
	"time"
)

// Hook up gocheck into the gotest runner.
func Test(t *testing.T) { TestingT(t) }

type ConnectionSuite struct{}

var _ = Suite(&ConnectionSuite{})

func testPacket(c *C, given, should []string) {
	c.Check(len(given), Equals, len(should))
	for i := 0; i < len(given); i += 1 {
		c.Check(given[i], Equals, should[i])
	}
}

type TestReader struct {
	*bytes.Buffer

	deadline time.Time
}

func NewReader(s string) *TestReader {
	rv := &TestReader{bytes.NewBufferString(s), time.Now().Add(100 * time.Hour)}
	return rv
}

func (tr *TestReader) SetDeadline(t time.Time) error {
	tr.deadline = t
	return nil
}
func (tr *TestReader) Read(p []byte) (int, error) {
	if time.Now().After(tr.deadline) {
		return 0, fmt.Errorf("Timeout")
	}
	n, err := tr.Buffer.Read(p)
	if err == io.EOF {
		return n, nil
	}
	return n, err
}

func (s *ConnectionSuite) TestReadSimplePacket(c *C) {
	con := NewConnection(NewReader("\x00\x06aaaa"))
	p, err := con.readPacket()
	c.Assert(err, Equals, nil)
	testPacket(c, p, []string{"aaaa"})
}

func (s *ConnectionSuite) TestReadSimplePacket1(c *C) {
	con := NewConnection(NewReader("\x00\x0faaaa\x00bbb\x00cc\x00d"))
	p, err := con.readPacket()
	c.Assert(err, Equals, nil)
	testPacket(c, p, []string{"aaaa", "bbb", "cc", "d"})
}

func (s *ConnectionSuite) TestReadPacketTooShortTimingOut(c *C) {
	con := NewConnection(NewReader("\xff"))
	p, err := con.readPacket()
	c.Assert(err, Not(Equals), nil)
	testPacket(c, p, []string{})
}
