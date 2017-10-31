package mysqlops

import (
	"database/sql"
	"fmt"

	// Don't import MySQL driver. All access is via database/sql.
	_ "github.com/go-sql-driver/mysql"
)

// Database is a database managed by the operator.
type Database struct {
	Name  string
	Owner string
}

// Config is the database connection configuration.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

// MySQLOps has the database handle for connecting to the database.
type MySQLOps struct {
	MySqlDB *sql.DB
}

// New creates the connection to the database.
func New(config Config) (*MySQLOps, error) {
	connStr := fmt.Sprintf("%s:%s@tcp(%s:%d)/", config.User, config.Password, config.Host, config.Port)
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("creating MySQL client: %s", err)
	}

	mysqlOps := &MySQLOps{
		MySqlDB: db,
	}

	return mysqlOps, nil
}

// CreateDatabase creates a MySQL database.
func (m *MySQLOps) CreateDatabase(name, owner string) error {
	createDb := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", name)
	_, err := m.MySqlDB.Exec(createDb)
	if err != nil {
		return fmt.Errorf("creating database: %s", err)
	}

	return nil
}

func (m *MySQLOps) ChangeDatabaseOwner(name, owner string) error {
	return nil
}

// DeleteDatabase deletes a MySQL database.
func (m *MySQLOps) DeleteDatabase(name string) error {
	deleteDb := fmt.Sprintf("DROP DATABASE `%s`", name)
	_, err := m.MySqlDB.Exec(deleteDb)
	if err != nil {
		return fmt.Errorf("deleting database: %s", err)
	}

	return nil
}

// ListDatabases lists the MySQL databases.
func (m *MySQLOps) ListDatabases() ([]Database, error) {
	dbs := []Database{}

	rows, err := m.MySqlDB.Query("SHOW DATABASES")
	if err != nil {
		return []Database{}, fmt.Errorf("listing databases: %s", err)
	}

	defer rows.Close()

	var dbName string

	for rows.Next() {
		err := rows.Scan(&dbName)
		if err != nil {
			return []Database{}, fmt.Errorf("getting database name: %s", err)
		}

		dbs = append(dbs, Database{Name: dbName})
	}

	return dbs, nil
}
