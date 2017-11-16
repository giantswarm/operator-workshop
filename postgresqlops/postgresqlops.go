package postgresqlops

import (
	"database/sql"
	"fmt"

	// Don't import PostgreSQL driver. All access is via database/sql.
	_ "github.com/lib/pq"
)

// Database is a database managed by the operator.
type Database struct {
	Name  string
	Owner string
}

// Config is the database connection configuration.
type Config struct {
	Host string
	Port int

	User     string
	Password string
}

// PostgreSQLOps has the database handle for connecting to the database.
type PostgreSQLOps struct {
	db *sql.DB
}

// New creates the connection to the database.
func New(config Config) (*PostgreSQLOps, error) {
	// Postgres user and password are hardcoded and match the resources in postgres.yaml.
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s sslmode=disable", config.Host, config.Port, config.User, config.Password)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("creating postgres client: %s", err)
	}

	postgreSQLOps := &PostgreSQLOps{
		db: db,
	}

	return postgreSQLOps, nil
}

// Close relases all PosgreSQLOps resources.
func (p *PostgreSQLOps) Close() error {
	return p.db.Close()
}

// CreateDatabase creates a database and owner if they don't exist.
func (p *PostgreSQLOps) CreateDatabase(name, owner string) error {
	ownerExists, err := p.hasUser(owner)
	if err != nil {
		return fmt.Errorf("checking owner exists: %s", err)
	}
	if !ownerExists {
		p.createUser(owner)
	}

	dbExists, err := p.hasDatabase(name)
	if err != nil {
		return fmt.Errorf("checking database exists: %s", err)
	}
	if !dbExists {
		createDb := fmt.Sprintf("CREATE DATABASE \"%s\"", name)
		_, err := p.db.Exec(createDb)
		if err != nil {
			return fmt.Errorf("creating database: %s", err)
		}
	}

	return nil
}

// ChangeDatabaseOwner changes the database owner and creates the user if it
// doesn't exist.
func (p *PostgreSQLOps) ChangeDatabaseOwner(name, owner string) error {
	ownerExists, err := p.hasUser(owner)
	if err != nil {
		return fmt.Errorf("checking owner exists: %s", err)
	}
	if !ownerExists {
		p.createUser(owner)
	}

	changeOwner := fmt.Sprintf("ALTER DATABASE \"%s\" OWNER TO \"%s\"", name, owner)
	_, err = p.db.Exec(changeOwner)
	if err != nil {
		return fmt.Errorf("changing owner: %s", err)
	}

	return nil
}

// DeleteDatabase deletes a database if it exists.
func (p *PostgreSQLOps) DeleteDatabase(name string) error {
	dbExists, err := p.hasDatabase(name)
	if err != nil {
		return fmt.Errorf("checing database exists: %s", err)
	}

	if dbExists {
		deleteDb := fmt.Sprintf("DROP DATABASE \"%s\"", name)
		_, err := p.db.Exec(deleteDb)
		if err != nil {
			return fmt.Errorf("deleting database: %s", err)
		}
	}

	return nil
}

// ListDatabases lists the databases.
func (p *PostgreSQLOps) ListDatabases() ([]Database, error) {
	dbs := []Database{}

	rows, err := p.db.Query("SELECT pg_database.datname, pg_user.usename FROM pg_database, pg_user WHERE pg_database.datdba = pg_user.usesysid AND pg_database.datname NOT IN ('postgres', 'template0', 'template1')")
	if err != nil {
		return []Database{}, fmt.Errorf("listing databases: %s", err)
	}

	defer rows.Close()

	var dbName, owner string

	for rows.Next() {
		err := rows.Scan(&dbName, &owner)
		if err != nil {
			return []Database{}, fmt.Errorf("getting database values: %s", err)
		}

		dbs = append(dbs, Database{Name: dbName, Owner: owner})
	}

	return dbs, nil
}

func (p *PostgreSQLOps) hasDatabase(name string) (bool, error) {
	dbs, err := p.ListDatabases()
	if err != nil {
		return false, fmt.Errorf("checking database exists: %s", err)
	}

	for _, db := range dbs {
		if db.Name == name {
			return true, nil
		}
	}

	return false, nil
}

func (p *PostgreSQLOps) createUser(user string) error {
	createUser := fmt.Sprintf("CREATE USER \"%s\" WITH CREATEDB", user)
	_, err := p.db.Exec(createUser)
	if err != nil {
		return fmt.Errorf("creating user: %s", err)
	}

	return nil
}

func (p *PostgreSQLOps) hasUser(name string) (bool, error) {
	rows, err := p.db.Query("SELECT pg_user.usename FROM pg_user")
	if err != nil {
		return false, fmt.Errorf("listing users: %s", err)
	}

	defer rows.Close()

	var user string

	for rows.Next() {
		err := rows.Scan(&user)
		if err != nil {
			return false, fmt.Errorf("getting database values: %s", err)
		}

		if user == name {
			return true, nil
		}
	}

	return false, nil
}
