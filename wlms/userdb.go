package main

import (
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	_ "github.com/ziutek/mymysql/godrv"
	"io"
	"log"
	"crypto/rand"
)

type UserDb interface {
	ContainsName(name string) bool
	PasswordCorrect(name, password string) bool
	GenerateChallengeResponsePairFromUsername(name string) (string, string, bool)
	GenerateDowngradedUserNonce(registeredName, assignedName string) string
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
	h := sha1.New()
	io.WriteString(h, password)
	passwordHash := h.Sum(nil)

	i.users[name] = user{hex.EncodeToString(passwordHash), perms}
}

func (i InMemoryUserDb) ContainsName(name string) bool {
	_, ok := i.users[name]
	return ok
}

func (i InMemoryUserDb) PasswordCorrect(name, password string) bool {
	if !i.ContainsName(name) {
		return false
	}
	h := sha1.New()
	io.WriteString(h, password)
	passwordHash := h.Sum(nil)

	return i.users[name].password == hex.EncodeToString(passwordHash)
}

func GenerateChallengeResponsePairFromSecret(passwordHash string) (string, string, bool) {
	nonce := make([]byte, 16)
	_, err := rand.Read(nonce)
	if err != nil {
		log.Printf("Error when trying to create random nonce for login: %v", err)
		return "", "", false
	}
	challenge := hex.EncodeToString(nonce)

	h := sha1.New()
	io.WriteString(h, challenge)
	io.WriteString(h, passwordHash)
	response := hex.EncodeToString(h.Sum(nil))

	return challenge, response, true
}

func (i InMemoryUserDb) GenerateChallengeResponsePairFromUsername(name string) (string, string, bool) {
	if !i.ContainsName(name) {
		return "", "", false
	}
	return GenerateChallengeResponsePairFromSecret(i.users[name].password)
}

func (i InMemoryUserDb) GenerateDowngradedUserNonce(registeredName, assignedName string) string {
	if !i.ContainsName(registeredName) {
		log.Printf("Error: Asked to create nonce for unregistered user")
		return "unregistered"
	}

	h := sha1.New()
	io.WriteString(h, assignedName)
	io.WriteString(h, i.users[registeredName].password)
	return hex.EncodeToString(h.Sum(nil))
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
	if con.Ping() != nil {
		log.Fatal("Database closed connection immediately.")
	}
	return &SqlDatabase{con}
}

func (db *SqlDatabase) Close() {
	if db.db != nil {
		db.db.Close()
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

func (db *SqlDatabase) retrievePasswordHash(name string) []byte {
	var id int64
	if err := db.db.QueryRow("select id from auth_user where username=?", name).Scan(&id); err != nil {
		return nil
	}
	var golden string
	if err := db.db.QueryRow("select password from wlggz_ggzauth where user_id=?", id).Scan(&golden); err != nil {
		return nil
	}

	goldenHash, err := base64.StdEncoding.DecodeString(golden)
	if err != nil {
		return nil
	}
	return goldenHash
}

func (db *SqlDatabase) PasswordCorrect(name, password string) bool {

	goldenHash := db.retrievePasswordHash(name)
	if goldenHash == nil {
		return false
	}

	h := sha1.New()
	io.WriteString(h, password)
	givenHash := h.Sum(nil)

	return string(goldenHash) == string(givenHash)
}

func (db *SqlDatabase) GenerateChallengeResponsePairFromUsername(name string) (string, string, bool) {
	goldenHash := db.retrievePasswordHash(name)
	if goldenHash == nil {
		return "", "", false
	}
	return GenerateChallengeResponsePairFromSecret(hex.EncodeToString(goldenHash))
}

func (db *SqlDatabase) GenerateDowngradedUserNonce(registeredName, assignedName string) string {
	goldenHash := db.retrievePasswordHash(registeredName)
	if goldenHash == nil {
		log.Printf("Error: Asked to create nonce for unregistered user")
		return "unregistered"
	}

	h := sha1.New()
	io.WriteString(h, assignedName)
	io.WriteString(h, hex.EncodeToString(goldenHash))
	return hex.EncodeToString(h.Sum(nil))
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
