package test_utils

import (
	"io"
	"net"
)

type FakeListener struct {
	connections []*FakeConn
}

func NewFakeListener(connections []FakeConn) FakeListener {
	pconn := make([]*FakeConn, len(connections))
	for i := range pconn {
		pconn[i] = &connections[i]
	}
	return FakeListener{pconn}
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
