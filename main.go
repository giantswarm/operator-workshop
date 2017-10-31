package main

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"github.com/giantswarm/operator-workshop/postgresops"
)

// PostgresConfigSpec is custom object specification. Represents the desired state
// towards which the operator reconciles. It also includes information
// necessary to perform the reconciliation, i.e. database access information.
type PostgresConfigSpec struct {
	// Service is service name which points to a Postgres server.
	Service string `json:"service"`
	// Port is the Postgres server listen port.
	Port int `json:"port"`

	// Database is database name to be created.
	Database string `json:"database"`
	// Owner is the database owner.
	Owner string `json:"owner"`
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("I ")
}

func main() {
	err := mainWithError()
	if err != nil {
		log.Fatalf("error: %#v", err)
	}
}

func mainWithError() error {
	postgresHostname, err := getHostname()
	if err != nil {
		return fmt.Errorf("getting postgres hostname: %s", err)
	}

	postgresPort, err := getServicePort("workshop-postgres")
	if err != nil {
		return fmt.Errorf("getting postgres hostname: %s", err)
	}

	configSpec := PostgresConfigSpec{
		Service:  postgresHostname,
		Port:     postgresPort,
		Database: "operator_workshop",
		Owner:    "operator",
	}

	pgConfig := postgresops.Config{
		Host: configSpec.Service,
		Port: configSpec.Port,
	}

	postgresOps, err := postgresops.New(pgConfig)
	if err != nil {
		return fmt.Errorf("creating PostgresOps: %s", err)
	}

	err = postgresOps.CreateDatabase(configSpec.Database, configSpec.Owner)
	if err != nil {
		return fmt.Errorf("creating database: %s", err)
	}

	err = postgresOps.ChangeDatabaseOwner(configSpec.Database, "new_owner")
	if err != nil {
		return fmt.Errorf("changing database owner: %s", err)
	}

	dbs, err := postgresOps.ListDatabases()
	if err != nil {
		return fmt.Errorf("listing databases: %s", err)
	}

	log.Printf("Listing %d databases", len(dbs))

	for _, db := range dbs {
		log.Printf("Database: %s Owner: %s", db.Name, db.Owner)
	}

	err = postgresOps.DeleteDatabase(configSpec.Database)
	if err != nil {
		return fmt.Errorf("delete database: %s", err)
	}

	return nil
}

func getHostname() (string, error) {
	out, err := exec.Command("minikube", "ip").Output()
	if err != nil {
		return "", fmt.Errorf("getting hostname: %s", err)
	}

	minikubeIP := strings.TrimSpace(string(out))

	return minikubeIP, nil
}

func getServicePort(serviceName string) (int, error) {
	out, err := exec.Command("minikube", "service", serviceName, "--format", "{{.Port}}").Output()
	if err != nil {
		return -1, fmt.Errorf("getting hostname: %s", err)
	}

	minikubePort := strings.TrimSpace(string(out))
	port, err := strconv.Atoi(minikubePort)
	if err != nil {
		return -1, fmt.Errorf("converting port to int: %s", err)
	}

	return port, nil
}
