package main

import (
	"errors"
	"github.com/widelands_metaserver/wlnr/rpc_data"
)

type RelayRPC struct {
	server *Server
}

func NewRelayRPC(s *Server) *RelayRPC {
	return &RelayRPC{
		server: s,
	}
}

func (rpc *RelayRPC) NewGame(in *rpc_data.NewGameData, ignored *bool) error {
	ret := rpc.server.CreateGame(in.Name, in.Password)
	if ret != true {
		return errors.New("Game already exists")
	}
	return nil
}

func (rpc *RelayRPC) Shutdown( *bool, *bool) error {
	rpc.server.InitiateShutdown()
	return nil
}
