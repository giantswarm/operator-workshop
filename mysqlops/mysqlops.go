package mysqlops

type Config struct {
}

type MySQLOps struct {
}

func New(config Config) (*MySQLOps, error) {
	mysqlOps := &MySQLOps{}

	return mysqlOps, nil
}

func (m *MySQLOps) CreateDatabase(name string) error {
	return nil
}
