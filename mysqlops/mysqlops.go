package mysqlops

type Database struct {
	Name  string
	Owner string
}

type Config struct {
}

type MySQLOps struct {
}

func New(config Config) (*MySQLOps, error) {
	mysqlOps := &MySQLOps{}

	return mysqlOps, nil
}

func (m *MySQLOps) CreateDatabase(name, owner string) error {
	return nil
}

func (m *MySQLOps) ChangeDatabaseOwner(name, owner string) error {
	return nil
}

func (m *MySQLOps) DeleteDatabase(name string) error {
	return nil
}

func (m *MySQLOps) ListDatabases() ([]Database, error) {
	return []Database{}, nil
}
