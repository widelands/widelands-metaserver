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

// ContainsName() reports every registered name, so that nobody else can use
// it. All other lookups only consider users who may log in, i.e. whose
// website account is active and not deleted.
type UserDb interface {
	ContainsName(name string) bool
	GenerateChallengeResponsePairFromUsername(name string) (string, string, bool)
	GenerateDowngradedUserNonce(registeredName, assignedName string) string
	Permissions(name string) Permissions
	Close()
}

type user struct {
	password    string
	permissions Permissions
	isActive    bool
	deleted     bool
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

	i.users[name] = user{hex.EncodeToString(passwordHash), perms, true, false}
}

// Mirrors auth_user.is_active and wlprofile_profile.deleted on the website.
func (i *InMemoryUserDb) SetAccountStatus(name string, isActive, deleted bool) {
	u := i.users[name]
	u.isActive = isActive
	u.deleted = deleted
	i.users[name] = u
}

func (i InMemoryUserDb) ContainsName(name string) bool {
	_, ok := i.users[name]
	return ok
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
	if !i.mayLogIn(name) {
		return "", "", false
	}
	return GenerateChallengeResponsePairFromSecret(i.users[name].password)
}

func (i InMemoryUserDb) GenerateDowngradedUserNonce(registeredName, assignedName string) string {
	if !i.mayLogIn(registeredName) {
		log.Printf("Error: Asked to create nonce for unregistered user")
		return "unregistered"
	}

	h := sha1.New()
	io.WriteString(h, assignedName)
	io.WriteString(h, i.users[registeredName].password)
	return hex.EncodeToString(h.Sum(nil))
}

func (i InMemoryUserDb) Permissions(name string) Permissions {
	if !i.mayLogIn(name) {
		return UNREGISTERED
	}
	return i.users[name].permissions
}

func (i InMemoryUserDb) mayLogIn(name string) bool {
	u, ok := i.users[name]
	return ok && u.isActive && !u.deleted
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

// Returns nil if the user has no password or the website account is inactive
// or deleted. A missing profile is treated like a deleted one.
func (db *SqlDatabase) retrievePasswordHash(name string) []byte {
	var golden string
	if err := db.db.QueryRow(
		"select g.password from auth_user u"+
			" join wlprofile_profile p on p.user_id=u.id"+
			" join wlggz_ggzauth g on g.user_id=u.id"+
			" where u.username=? and u.is_active=1 and p.deleted=0", name).Scan(&golden); err != nil {
		return nil
	}

	goldenHash, err := base64.StdEncoding.DecodeString(golden)
	if err != nil {
		return nil
	}
	return goldenHash
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
	var permission int64
	if err := db.db.QueryRow(
		"select g.permissions from auth_user u"+
			" join wlprofile_profile p on p.user_id=u.id"+
			" join wlggz_ggzauth g on g.user_id=u.id"+
			" where u.username=? and u.is_active=1 and p.deleted=0", name).Scan(&permission); err != nil {
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
