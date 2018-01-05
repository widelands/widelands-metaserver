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
		log.Printf("Error: Ordered to create game '%v', but it already exists", in.Name)
		return errors.New("Game already exists")
	}
	*success = true
	return nil
}
