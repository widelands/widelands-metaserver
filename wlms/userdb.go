package main

type UserDb interface {
	ContainsName(name string) bool
	PasswordCorrect(name, password string) bool
	Permissions(name string) Permissions
}

type User struct {
	password    string
	permissions Permissions
}

type InMemoryUserDb struct {
	users map[string]User
}

func NewInMemoryDb() *InMemoryUserDb {
	return &InMemoryUserDb{make(map[string]User)}
}

func (i *InMemoryUserDb) AddUser(name string, password string, perms Permissions) {
	i.users[name] = User{password, perms}
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
