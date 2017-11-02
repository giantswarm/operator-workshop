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
}

// PostgresqlOps has the database handle for connecting to the database.
type PostgresqlOps struct {
	PostgresqlDB *sql.DB
}

// New creates the connection to the database.
func New(config Config) (*PostgresqlOps, error) {
	// Postgres user and password are hardcoded and match the resources in postgres.yaml.
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s sslmode=disable", config.Host, config.Port, "postgres", "operator-workshop")

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("creating postgres client: %s", err)
	}

	postgresqlOps := &PostgresqlOps{
		PostgresqlDB: db,
	}

	return postgresqlOps, nil
}

// CreateDatabase creates a database and owner if they don't exist.
func (p *PostgresqlOps) CreateDatabase(name, owner string) error {
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
		_, err := p.PostgresqlDB.Exec(createDb)
		if err != nil {
			return fmt.Errorf("creating database: %s", err)
		}
	}

	return nil
}

// ChangeDatabaseOwner changes the database owner and creates the user if it
// doesn't exist.
func (p *PostgresqlOps) ChangeDatabaseOwner(name, owner string) error {
	ownerExists, err := p.hasUser(owner)
	if err != nil {
		return fmt.Errorf("checking owner exists: %s", err)
	}
	if !ownerExists {
		p.createUser(owner)
	}

	changeOwner := fmt.Sprintf("ALTER DATABASE \"%s\" OWNER TO \"%s\"", name, owner)
	_, err = p.PostgresqlDB.Exec(changeOwner)
	if err != nil {
		return fmt.Errorf("changing owner: %s", err)
	}

	return nil
}

// DeleteDatabase deletes a database if it exists.
func (p *PostgresqlOps) DeleteDatabase(name string) error {
	dbExists, err := p.hasDatabase(name)
	if err != nil {
		return fmt.Errorf("checing database exists: %s", err)
	}

	if dbExists {
		deleteDb := fmt.Sprintf("DROP DATABASE \"%s\"", name)
		_, err := p.PostgresqlDB.Exec(deleteDb)
		if err != nil {
			return fmt.Errorf("deleting database: %s", err)
		}
	}

	return nil
}

// ListDatabases lists the databases.
func (p *PostgresqlOps) ListDatabases() ([]Database, error) {
	dbs := []Database{}

	rows, err := p.PostgresqlDB.Query("SELECT pg_database.datname, pg_user.usename FROM pg_database, pg_user WHERE pg_database.datdba = pg_user.usesysid")
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

func (p *PostgresqlOps) hasDatabase(name string) (bool, error) {
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

func (p *PostgresqlOps) createUser(user string) error {
	createUser := fmt.Sprintf("CREATE USER \"%s\" WITH CREATEDB", user)
	_, err := p.PostgresqlDB.Exec(createUser)
	if err != nil {
		return fmt.Errorf("creating user: %s", err)
	}

	return nil
}

func (p *PostgresqlOps) hasUser(name string) (bool, error) {
	rows, err := p.PostgresqlDB.Query("SELECT pg_user.usename FROM pg_user")
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
