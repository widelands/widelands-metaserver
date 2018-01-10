package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
)

type Config struct {
	Database, User, Password, Table, Backend, IRCServer, Nickname, Realname, Channel string
	UseTLS                                                                           bool
}

func (l *Config) ConfigFrom(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &l)
}

func main() {
	var config string
	flag.StringVar(&config, "config", "", "Database configuration file to read.")
	flag.Parse()

	var db UserDb
	var ircbridge IRCBridger
	if config != "" {
		log.Println("Loading configuration")
		var cfg Config
		if err := cfg.ConfigFrom(config); err != nil {
			log.Fatalf("Could not parse config file: %v", err)
		}
		if cfg.Backend == "mysql" {
			db = NewMySqlDatabase(cfg.Database, cfg.User, cfg.Password, cfg.Table)
		} else {
			db = NewInMemoryDb()
		}
		ircbridge = NewIRCBridge(cfg.IRCServer, cfg.Realname, cfg.Nickname, cfg.Channel, cfg.UseTLS)
	} else {
		log.Println("No configuration found, using in-memory database")
		db = NewInMemoryDb()
	}
	defer db.Close()

	channels := NewIRCBridgerChannels()
	if ircbridge != nil {
		ircbridge.Connect(channels)
	}
	RunServer(db, channels)

}
