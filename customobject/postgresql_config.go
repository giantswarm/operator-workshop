package customobject

import "fmt"

// PostgreSQLConfig is custom object of postgresqlconfigs.containerconf.de custom
// resource.
type PostgreSQLConfig struct {
	Spec PostgreSQLConfigSpec `json:"spec"`
}

func (m PostgreSQLConfig) Validate() error {
	if err := m.Spec.Validate(); err != nil {
		return fmt.Errorf("spec is not valid: %s", err)
	}
	return nil
}

// PostgreSQLConfigSpec is custom object specification. Represents the desired state
// towards which the operator reconciles. It also includes information
// necessary to perform the reconciliation, i.e. database access information.
type PostgreSQLConfigSpec struct {
	// Database is database name to be created.
	Database string `json:"database"`
	// Owner is the database owner.
	Owner string `json:"owner"`
}

func (s PostgreSQLConfigSpec) Validate() error {
	if s.Database == "" {
		return fmt.Errorf("database is not set")
	}
	if s.Owner == "" {
		return fmt.Errorf("owner is not set")
	}
	return nil
}
