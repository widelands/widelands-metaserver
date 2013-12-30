package main

import (
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"fmt"
	_ "github.com/ziutek/mymysql/godrv"
	"io"
	"log"
)

type UserDb interface {
	ContainsName(name string) bool
	PasswordCorrect(name, password string) bool
	Permissions(name string) Permissions
	Close()
}

type user struct {
	password    string
	permissions Permissions
}

type InMemoryUserDb struct {
	users map[string]user
}

func NewInMemoryDb() *InMemoryUserDb {
	return &InMemoryUserDb{make(map[string]user)}
}

func (i *InMemoryUserDb) AddUser(name string, password string, perms Permissions) {
	i.users[name] = user{password, perms}
}

func (i InMemoryUserDb) ContainsName(name string) bool {
	_, ok := i.users[name]
	return ok
}

func (i InMemoryUserDb) PasswordCorrect(name, password string) bool {
	if !i.ContainsName(name) {
		return false
	}
	return i.users[name].password == password
}

func (i InMemoryUserDb) Permissions(name string) Permissions {
	if !i.ContainsName(name) {
		return UNREGISTERED
	}
	return i.users[name].permissions
}

func (i InMemoryUserDb) Close() {
}

type SqlDatabase struct {
	db *sql.DB
}

func NewMySqlDatabase(database, user, password, table string) *SqlDatabase {
	s := fmt.Sprintf("%s*%s/%s/%s", database, table, user, password)
	con, err := sql.Open("mymysql", s)
	if err != nil {
		log.Fatal("Could not connect to database.")
	}
	return &SqlDatabase{con}
}

func (db *SqlDatabase) Close() {
	if db.db != nil {
		db.Close()
		db.db = nil
	}
}

func (db *SqlDatabase) ContainsName(name string) bool {
	var id int
	err := db.db.QueryRow("select id from auth_user where username=?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return false
	}
	return true
}

func (db *SqlDatabase) PasswordCorrect(name, password string) bool {
	var id int64
	if err := db.db.QueryRow("select id from auth_user where username=?", name).Scan(&id); err != nil {
		return false
	}
	var golden string
	if err := db.db.QueryRow("select password from wlggz_ggzauth where user_id=?", id).Scan(&golden); err != nil {
		return false
	}

	h := sha1.New()
	io.WriteString(h, password)
	givenHash := h.Sum(nil)

	goldenHash, err := base64.StdEncoding.DecodeString(golden)
	if err != nil {
		return false
	}
	return string(goldenHash) == string(givenHash)
}

func (db *SqlDatabase) Permissions(name string) Permissions {
	var id int64
	if err := db.db.QueryRow("select id from auth_user where username=?", name).Scan(&id); err != nil {
		return UNREGISTERED
	}
	var permission int64
	if err := db.db.QueryRow("select permissions from wlggz_ggzauth where user_id=?", id).Scan(&permission); err != nil {
		return UNREGISTERED
	}

	// Historic values from ggz.
	switch permission {
	case 127:
		return SUPERUSER
	case 7:
		return REGISTERED
	default:
		return UNREGISTERED
	}
}
