package main

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// A server that accepts TCP connections but closes them right away (like a
// refused TLS handshake) must not be hammered with reconnects.
func TestIRCReconnectIsThrottled(t *testing.T) {
	oldMin, oldMax := ircReconnectMinDelay, ircReconnectMaxDelay
	ircReconnectMinDelay, ircReconnectMaxDelay = 200*time.Millisecond, time.Second
	defer func() { ircReconnectMinDelay, ircReconnectMaxDelay = oldMin, oldMax }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()

	bridge := NewIRCBridge(ln.Addr().String(), "IRCTest", "IRCTest", "#widelands-test", true)
	bridge.Connect(NewIRCBridgerChannels())
	// Expected attempts: at 0s, 0.2s, 0.6s and 1.4s.
	time.Sleep(1500 * time.Millisecond)

	if n := accepted.Load(); n < 2 || n > 5 {
		t.Fatalf("%d connection attempts in 1.5s, want 2 to 5", n)
	}
}
