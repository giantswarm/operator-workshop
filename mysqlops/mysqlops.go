package mysqlops

import (
	"errors"
	"sync"
)

// This is a fake implementaiton so it has global variables, which represent
// database server.
var (
	databases = make(map[string]Database)
	mux       = new(sync.Mutex)
)

type Database struct {
	Name  string
	Owner string
}

type Config struct {
}

// MySQLOps is a fake MySQL operatations. To be implemented.
type MySQLOps struct {
}

func New(config Config) (*MySQLOps, error) {
	mysqlOps := &MySQLOps{}

	return mysqlOps, nil
}

func (m *MySQLOps) CreateDatabase(name, owner string) error {
	mux.Lock()
	defer mux.Unlock()

	databases[name] = Database{
		Name:  name,
		Owner: owner,
	}

	return nil
}

func (m *MySQLOps) ChangeDatabaseOwner(name, owner string) error {
	mux.Lock()
	defer mux.Unlock()

	db, ok := databases[name]

	if !ok {
		return errors.New("not found")
	}

	db.Owner = owner
	databases[db.Name] = db

	return nil
}

func (m *MySQLOps) DeleteDatabase(name string) error {
	mux.Lock()
	defer mux.Unlock()

	_, ok := databases[name]

	if !ok {
		return errors.New("not found")
	}

	delete(databases, name)

	return nil
}

func (m *MySQLOps) ListDatabases() ([]Database, error) {
	mux.Lock()
	defer mux.Unlock()

	var dbs []Database
	for _, db := range databases {
		dbs = append(dbs, db)
	}

	return dbs, nil
}
