package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
)

type Config struct {
	Database, User, Password, Table, Backend, IRCServer, Nickname, Realname, Channel, Hostname string
	UseTLS                                                                                     bool
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
	var testuser bool
	flag.StringVar(&config, "config", "", "Configuration file to read.")
	flag.BoolVar(&testuser, "testuser", false, "Create a \"testuser\" with password \"test\" on startup. Only works with memory user database.")
	flag.Parse()

	var db UserDb
	var ircbridge IRCBridger
	hostname := "localhost"
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

		if cfg.Hostname != "" {
			hostname = cfg.Hostname
		}
	} else {
		log.Println("No configuration found, using in-memory database")
		db = NewInMemoryDb()
	}
	mdb, ok := db.(*InMemoryUserDb)
	if ok && testuser {
		log.Println("Creating testuser in memory user database")
		mdb.AddUser("testuser", "test", REGISTERED)
	}
	defer db.Close()
	channels := NewIRCBridgerChannels()
	if ircbridge != nil {
		ircbridge.Connect(channels)
	}
	RunServer(db, channels, hostname)

}
