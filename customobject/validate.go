package customobject

import "fmt"

<<<<<<< HEAD
<<<<<<< HEAD
func Validate(obj PostgreSQLConfig) error {
=======
func Validate(obj *PostgreSQLConfig) error {
>>>>>>> extract Validate function; move list object to solution1
=======
func Validate(obj PostgreSQLConfig) error {
>>>>>>> Validate takes a struct not a pointer
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
