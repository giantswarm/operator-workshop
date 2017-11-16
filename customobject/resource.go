package customobject

import (
	"fmt"

	"github.com/giantswarm/operator-workshop/postgresqlops"
)

// Resource represents a resource being a result of PostgreSQLConfig object
// reconciliation. In this case it is a database with owner set to a specified
// user.
type Resource struct {
	ops *postgresqlops.PostgreSQLOps
}

func NewResource(ops *postgresqlops.PostgreSQLOps) *Resource {
	return &Resource{
		ops: ops,
	}
}

// EnsureCreated is an idempotent method making sure the database resource is
// in a state described in the custom object.
func (r *Resource) EnsureCreated(obj *PostgreSQLConfig) (status string, err error) {
	dbs, err := r.ops.ListDatabases()
	if err != nil {
		return "", fmt.Errorf("listing databases: %s", err)
	}

	db, ok := findDB(dbs, obj.Spec.Database)

	if !ok {
		err := r.ops.CreateDatabase(obj.Spec.Database, obj.Spec.Owner)
		if err != nil {
			return "", fmt.Errorf("creating database: %s", err)
		}
		return "database created", nil
	}

	if db.Owner != obj.Spec.Owner {
		err := r.ops.ChangeDatabaseOwner(obj.Spec.Database, obj.Spec.Owner)
		if err != nil {
			return "", fmt.Errorf("chaning owner=%#q: %s", db.Owner, err)
		}
		return fmt.Sprintf("owner=%#q changed", db.Owner), nil
	}

	return "already created", nil
}

// EnsureCreated is an idempotent method making sure the database resource
// described in the custom object is deleted.
func (r *Resource) EnsureDeleted(obj *PostgreSQLConfig) (status string, err error) {
	dbs, err := r.ops.ListDatabases()
	if err != nil {
		return "", fmt.Errorf("listing databases: %s", err)
	}

	_, ok := findDB(dbs, obj.Spec.Database)

	if ok {
		err = r.ops.DeleteDatabase(obj.Spec.Database)
		if err != nil {
			return "", fmt.Errorf("deleting database: %s", err)
		}
		return "database deleted", nil
	}

	return "already deleted", nil
}

func findDB(dbs []postgresqlops.Database, name string) (postgresqlops.Database, bool) {
	for _, db := range dbs {
		if db.Name == name {
			return db, true
		}
	}
	return postgresqlops.Database{}, false
}
