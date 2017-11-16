package customobject

// PostgreSQLConfig is custom object of postgresqlconfigs.containerconf.de custom
// resource.
type PostgreSQLConfig struct {
	Spec PostgreSQLConfigSpec `json:"spec"`
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
