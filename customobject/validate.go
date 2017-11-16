package customobject

import "fmt"

func Validate(obj PostgreSQLConfig) error {
	if err := validateSpec(obj.Spec); err != nil {
		return fmt.Errorf("spec is not valid: %s", err)
	}
	return nil
}

func validateSpec(spec PostgreSQLConfigSpec) error {
	if spec.Database == "" {
		return fmt.Errorf("database is not set")
	}
	if spec.Owner == "" {
		return fmt.Errorf("owner is not set")
	}
	return nil
}
