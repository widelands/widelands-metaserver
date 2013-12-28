package main

import (
	"container/list"
	"fmt"
	"launchpad.net/wlmetaserver/wlms/packet"
	"log"
	"net"
	"strings"
	"time"
)

type Server struct {
	shutdownServer    chan bool
	serverHasShutdown chan bool
	clients           *list.List
	listener          net.Listener
	user_db           UserDb
	motd              string

	clientSendingTimeout time.Duration
	pingCycleTime        time.Duration
}

func (s *Server) Shutdown() error {
	s.shutdownServer <- true
	return nil
}

func (s *Server) WaitTillShutdown() {
	<-s.serverHasShutdown
}

func (s *Server) NrClients() int {
	return s.clients.Len()
}

func (s *Server) SetClientSendingTimeout(d time.Duration) {
	s.clientSendingTimeout = d
}

func (s *Server) SetPingCycleTime(d time.Duration) {
	s.pingCycleTime = d
}

func (s *Server) mainLoop() error {
	<-s.shutdownServer
	s.shutdown()
	return nil
}

func (s *Server) shutdown() error {
	for s.clients.Len() > 0 {
		e := s.clients.Front()
		e.Value.(*Client).Disconnect()
		s.clients.Remove(e)
	}
	s.listener.Close()
	s.serverHasShutdown <- true
	return nil
}

func (s *Server) acceptLoop() {
	log.Print("Starting Goroutine: acceptLoop")
	for {
		conn, err := s.listener.Accept()
		log.Printf("conn: %v, err: %v\n", conn, err)
		if err != nil {
			log.Printf("Error accepting a connection: %v", err)
			break
		}
		go s.dealWithClient(NewClient(conn))
	}
	log.Print("Ending Goroutine: acceptLoop")
}

type handlerFunction func(client *Client, packet *packet.Packet) (string, bool)

func (s *Server) dealWithPacket(cmdName string, hf handlerFunction, client *Client, packet *packet.Packet) bool {
	errString, disconnect := hf(client, packet)
	if errString != "" {
		client.SendPacket("ERROR", cmdName, errString)
	}
	if disconnect {
		client.Disconnect()
		return true
	}
	return false
}

func (s *Server) dealWithClient(client *Client) {
	log.Print("Starting Goroutine: dealWithClient")
	timeout_channel := make(chan bool)
	startToPingTimer := time.NewTimer(s.pingCycleTime)
	waitingForPong := false

	for done := false; !done; {
		time.AfterFunc(s.clientSendingTimeout, func() {
			timeout_channel <- true
		})
		select {
		case pkg, ok := <-client.DataStream:
			if !ok {
				done = true
			} else {
				// TODO(sirver): this should probably use some kind of map
				waitingForPong = false
				startToPingTimer.Reset(s.pingCycleTime)
				cmdName, err := pkg.ReadString()
				if err != nil {
					log.Printf("ReadString returned an error: %v", err)
					done = true
				}
				switch cmdName {
				case "CHAT":
					done = s.dealWithPacket(cmdName, s.handleCHAT, client, pkg)
				case "LOGIN":
					done = s.dealWithPacket(cmdName, s.handleLOGIN, client, pkg)
				case "MOTD":
					done = s.dealWithPacket(cmdName, s.handleMOTD, client, pkg)
				case "DISCONNECT":
					reason, _ := pkg.ReadString()
					log.Printf("%s has disconnected with reason %s.", client.Name(), reason)
					client.Disconnect()
					done = true
				case "PONG":
					// do nothing
				default:
					client.SendPacket("ERROR", "GARBAGE_RECEIVED", "INVALID_CMD")
					client.Disconnect()
					done = true
				}
			}
		case <-timeout_channel:
			// NOCOM(sirver): refactor into method
			client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
			client.Disconnect()
			done = true
		case <-startToPingTimer.C:
			if waitingForPong {
				// NOCOM(sirver): refactor into method
				client.SendPacket("DISCONNECT", "CLIENT_TIMEOUT")
				client.Disconnect()
				done = true
			}
			client.SendPacket("PING")
			waitingForPong = true
			startToPingTimer.Reset(s.pingCycleTime)
		}
	}
	log.Print("Ending Goroutine: dealWithClient")

	// NOCOM(sirver): this would actually need a mutex.
	for e := s.clients.Front(); e != nil; e = e.Next() {
		if e.Value.(*Client) == client {
			s.clients.Remove(e)
		}
	}
	s.broadcastToConnectedClients("CLIENTS_UPDATE")
}

func (s *Server) handleCHAT(client *Client, pkg *packet.Packet) (string, bool) {
	message, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	// Sanitize message.
	message = strings.Replace(message, "<", "&lt;", -1)
	receiver, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	if len(receiver) == 0 {
		s.broadcastToConnectedClients("CHAT", client.Name(), message, "public")
	} else {
		recv_client := s.isLoggedIn(receiver)
		if recv_client != nil {
			recv_client.SendPacket("CHAT", client.Name(), message, "private")
		}
	}
	return "", false
}

func (s *Server) isLoggedIn(name string) *Client {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.Name() == name {
			return client
		}
	}
	return nil
}

func (s *Server) handleMOTD(client *Client, pkg *packet.Packet) (string, bool) {
	message, err := pkg.ReadString()
	if err != nil {
		return err.Error(), false
	}

	if client.Permissions() != SUPERUSER {
		return "DEFICIENT_PERMISSION", false
	}
	s.motd = message
	s.broadcastToConnectedClients("CHAT", "", s.motd, "system")

	return "", false
}

func (s *Server) handleLOGIN(client *Client, pkg *packet.Packet) (string, bool) {
	protocolVersion, err := pkg.ReadInt()
	if err != nil {
		return err.Error(), true
	}
	if protocolVersion != 0 {
		return "UNSUPPORTED_PROTOCOL", true
	}

	userName, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}

	buildId, err := pkg.ReadString()
	if err != nil {
		return err.Error(), true
	}

	isRegisteredOnServer, err := pkg.ReadBool()
	if err != nil {
		return err.Error(), true
	}

	if isRegisteredOnServer {
		if s.isLoggedIn(userName) != nil {
			return "ALREADY_LOGGED_IN", true
		}
		if !s.user_db.ContainsName(userName) {
			return "WRONG_PASSWORD", true
		}
		password, err := pkg.ReadString()
		if err != nil {
			return err.Error(), true
		}
		if !s.user_db.PasswordCorrect(userName, password) {
			return "WRONG_PASSWORD", true
		}
		client.SetPermissions(s.user_db.Permissions(userName))
	} else {
		baseName := userName
		for i := 1; s.user_db.ContainsName(userName) || s.isLoggedIn(userName) != nil; i++ {
			userName = fmt.Sprintf("%s%d", baseName, i)
		}
	}

	client.SetBuildId(buildId)
	client.SetName(userName)
	client.SetLoginTime(time.Now())
	client.SetState(CONNECTED)

	client.SendPacket("LOGIN", userName, client.Permissions().String())
	client.SendPacket("TIME", int(time.Now().Unix()))
	s.clients.PushBack(client)
	s.broadcastToConnectedClients("CLIENTS_UPDATE")

	if len(s.motd) != 0 {
		client.SendPacket("CHAT", "", s.motd, "system")
	}

	return "", false
}

func (s *Server) broadcastToConnectedClients(data ...interface{}) {
	for e := s.clients.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if client.State() == CONNECTED {
			client.SendPacket(data...)
		}
	}
}

func CreateServer() *Server {
	ln, err := net.Listen("tcp", ":7395") // TODO(sirver): softcode this
	if err != nil {
		log.Fatal(err)
	}
	// NOCOM(sirver): should use a proper database connection or flat file
	return CreateServerUsing(ln, NewInMemoryDb())
}

func CreateServerUsing(l net.Listener, db UserDb) *Server {
	server := &Server{
		shutdownServer:       make(chan bool),
		serverHasShutdown:    make(chan bool),
		clients:              list.New(),
		listener:             l,
		user_db:              db,
		clientSendingTimeout: time.Second * 30,
		pingCycleTime:        time.Second * 15,
	}
	go server.acceptLoop()
	go server.mainLoop()
	return server
}
