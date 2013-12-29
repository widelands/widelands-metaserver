package main

import (
	"flag"
)

func main() {
	var dbuser, dbpasswd, database string
	flag.StringVar(&database, "database", "inmemory", "Sql Database connection to use.")
	flag.StringVar(&dbuser, "dbuser", "", "User for mysql database.")
	flag.StringVar(&dbpasswd, "dbpasswd", "", "Password for user for mysql database.")
	flag.Parse()

	var db UserDb
	if database == "inmemory" {
		db = NewInMemoryDb()
	} else {
		db = NewMySqlDatabase(database, dbuser, dbpasswd)
	}
	defer db.Close()

	s := CreateServer(db)
	s.WaitTillShutdown()
}
