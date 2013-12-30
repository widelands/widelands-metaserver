package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
)

type Config struct {
	Database, User, Password, Table string
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
	if config != "" {
		var cfg Config
		if err := cfg.ConfigFrom(config); err != nil {
			log.Fatalf("Could not parse config file: %v", err)
		}
		db = NewMySqlDatabase(cfg.Database, cfg.User, cfg.Password, cfg.Table)
	} else {
		db = NewInMemoryDb()
	}
	defer db.Close()

	s := CreateServer(db)
	s.WaitTillShutdown()
}
