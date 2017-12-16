package main

import (
	"errors"
	"github.com/widelands/widelands_metaserver/wlnr/rpc_data"
	"log"
)

type RelayRPC struct {
	server *Server
}

func NewRelayRPC(s *Server) *RelayRPC {
	return &RelayRPC{
		server: s,
	}
}

func (rpc *RelayRPC) NewGame(in *rpc_data.NewGameData, success *bool) error {
	ret := rpc.server.CreateGame(in.Name, in.Password)
	if ret != true {
		return errors.New("Game already exists")
	}
	*success = true
	log.Printf("Starting new game named %v", in.Name)
	return nil
}
