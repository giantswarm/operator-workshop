package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/giantswarm/operator-workshop/postgresqlops"
)

const (
	dbServiceDefault  = "workshop-postgresql"
	dbUserDefault     = "postgres"
	dbPasswordDefault = "operator-workshop"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("I ")
}

type Config struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string

	K8sServer  string
	K8sCrtFile string
	K8sKeyFile string
	K8sCAFile  string
}

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

// PostgreSQLConfigList represents a list of custom objects. It is useful for
// decoding list API calls.
type PostgreSQLConfigList struct {
	Items []*PostgreSQLConfig `json:"items"`
}

func main() {
	ctx := context.Background()

	config := parseFlags()

	mainExitCodeCh := make(chan int)
	mainCtx, mainCancelFunc := context.WithCancel(ctx)

	// Run actual code.
	go func() {
		err := mainError(mainCtx, config)
		if err != nil {
			log.SetPrefix("E ")
			log.Printf("%s", err)
			mainExitCodeCh <- 1
		}
		mainExitCodeCh <- 0
	}()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	// Handle graceful stop.
	gracefulStop := false
	for {
		select {
		case code := <-mainExitCodeCh:
			log.Printf("exiting: code=%d", code)
			os.Exit(code)
		case sig := <-sigCh:
			// On second SIGKILL exit immediately.
			if sig == os.Kill && gracefulStop {
				log.Printf("exiting: forced exit code=1")
				os.Exit(1)
			}
			if !gracefulStop {
				log.Printf("exiting: trying to preform graceful stop")
				gracefulStop = true
				mainCancelFunc()
			}
		}
	}
}

func parseFlags() Config {
	var config Config

	var homeDir string
	{
		u, err := user.Current()
		if err != nil {
			homeDir = os.Getenv("HOME")
		} else {
			homeDir = u.HomeDir
		}

	}

	var minikubeIP string
	{
		out, err := exec.Command("minikube", "ip").Output()
		if err == nil {
			minikubeIP = strings.TrimSpace(string(out))
		}
	}

	var serverDefault string
	{
		if minikubeIP != "" {
			serverDefault = "https://" + string(minikubeIP) + ":8443"
		}
	}

	var dbPortDefault int
	{
		out, err := exec.Command("minikube", "service", dbServiceDefault, "--format", "{{.Port}}").Output()
		if err == nil {
			s := strings.TrimSpace(string(out))
			dbPortDefault, err = strconv.Atoi(s)
			if err != nil {
				dbPortDefault = 0
			}
		}
	}

	flag.StringVar(&config.DBHost, "postgresql.host", minikubeIP, "PostgreSQL server host.")
	flag.IntVar(&config.DBPort, "postgresql.port", dbPortDefault, "PostgreSQL server port.")
	flag.StringVar(&config.DBUser, "postgresql.user", dbUserDefault, "PostgreSQL user.")
	flag.StringVar(&config.DBPassword, "postgresql.password", dbPasswordDefault, "PostgreSQL password.")
	flag.StringVar(&config.K8sServer, "kubernetes.server", serverDefault, "Kubernetes API server address.")
	flag.StringVar(&config.K8sCrtFile, "kubernetes.crt", path.Join(homeDir, ".minikube/apiserver.crt"), "Kubernetes certificate file path.")
	flag.StringVar(&config.K8sKeyFile, "kubernetes.key", path.Join(homeDir, ".minikube/apiserver.key"), "Kubernetes key file path.")
	flag.StringVar(&config.K8sCAFile, "kubernetes.ca", path.Join(homeDir, ".minikube/ca.crt"), "Kubernetes CA file path.")
	flag.Parse()

	return config
}

func mainError(ctx context.Context, config Config) error {
	k8sClient, err := newHttpClient(config)
	if err != nil {
		return fmt.Errorf("creating K8s client: %s", err)
	}

	// Create Custom Resource Definition.
	{
		log.Printf("creating custom resource")

		// crdJson content in YAML format can be found in crd.yaml file.
		crdJson := `{
			"apiVersion": "apiextensions.k8s.io/v1beta1",
			"kind": "CustomResourceDefinition",
			"metadata": {
				"name": "postgresqlconfigs.containerconf.de"
			},
			"spec": {
				"group": "containerconf.de",
				"version": "v1",
				"scope": "Namespaced",
				"names": {
					"plural": "postgresqlconfigs",
					"singular": "postgresqlconfig",
					"kind": "PostgreSQLConfig",
					"shortNames": []
				}
			}
		}`

		url := config.K8sServer + "/apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions"
		res, err := k8sClient.Post(url, "application/json", strings.NewReader(crdJson))
		if err != nil {
			return fmt.Errorf("creating custom resource: requesting url=%s: %s", url, err)
		}

		body := readerToBytesTrimSpace(res.Body)
		res.Body.Close()

		if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
			log.Printf("creating custom resource: created")
		} else {
			alreadyExists := false
			errStr := "bad status"

			// Check if already exists.
			if res.StatusCode == http.StatusConflict {
				r, err := isStatusAlreadyExists(body)
				if err != nil {
					errStr = err.Error()
				} else {
					alreadyExists = r
				}
			}

			if alreadyExists {
				log.Printf("creating custom resource: already exists")
			} else {
				return fmt.Errorf("creating custom resource: %s status=%d body=%#q", errStr, res.StatusCode, body)
			}
		}
	}

	// Wait for the Custom Resource to be ready.
	{
		attempt := 1
		maxAttempts := 10
		checkInterval := time.Millisecond * 200

		for ; ; attempt++ {
			log.Printf("checking custom resource readiness attempt=%d", attempt)

			url := config.K8sServer + "/apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions"
			res, err := k8sClient.Get(url)
			if err != nil {
				return fmt.Errorf("checking custom resource readiness attempt=%d url=%s: %s", attempt, url, err)
			}

			body := readerToBytesTrimSpace(res.Body)
			res.Body.Close()

			if res.StatusCode == http.StatusOK {
				log.Printf("checking custom resource readiness attempt=%d: ready", attempt)
				break
			}

			if attempt == maxAttempts {
				return fmt.Errorf("checking custom resource readiness attempt=%d: bad status status=%d body=%#q", attempt, res.StatusCode, body)
			}

			log.Printf("checking custom resource readiness attempt=%d: not ready yet", attempt)
			time.Sleep(checkInterval)
		}
	}

	// Create PostgreSQLOps.
	var ops *postgresqlops.PostgreSQLOps
	{
		config := postgresqlops.Config{
			Host:     config.DBHost,
			Port:     config.DBPort,
			User:     config.DBUser,
			Password: config.DBPassword,
		}

		ops, err = postgresqlops.New(config)
		if err != nil {
			return fmt.Errorf("creating PostgreSQLOps: %s", err)
		}

		defer ops.Close()
	}

	// Start reconciliation loop. In every iteration the operator lists
	// current custom objects and reconciles towards the state described in
	// them. The loop is inifinite, can be cancelled with cancelling the
	// context.
	reconciliationCnt := 1
	reconciliationInterval := time.Second * 1
	for ; ; reconciliationCnt++ {
		log.Printf("reconciling loopCnt=%d", reconciliationCnt)

		if ctx.Err() == context.Canceled {
			log.Printf("reconciling loopCnt=%d: context cancelled", reconciliationCnt)
			return nil
		}

		url := config.K8sServer + "/apis/containerconf.de/v1/postgresqlconfigs"
		res, err := k8sClient.Get(url)
		if err != nil {
			return fmt.Errorf("reconciling loopCnt=%d: %s url=%s", reconciliationCnt, err, url)
		}

		body := readerToBytesTrimSpace(res.Body)
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("reconciling loopCnt=%d: error client response status status=%d body=%#q", reconciliationCnt, res.StatusCode, body)
			time.Sleep(reconciliationInterval)
			continue
		}

		var configs PostgreSQLConfigList
		err = json.Unmarshal(body, &configs)
		if err != nil {
			log.Printf("reconciling loopCnt=%d: error unmarshalling postgresqlconfigs list: %s body=%#q", reconciliationCnt, err, body)
			time.Sleep(reconciliationInterval)
			continue
		}

		// Many DB operations are repeated. This can be
		// optimised but it isn't really an issue.
		dbs, err := ops.ListDatabases()
		if err != nil {
			log.Printf("reconciling loopCnt=%d: error listing databases: %s", reconciliationCnt, err)
			time.Sleep(reconciliationInterval)
			continue
		}

		// Reconcile updates and memorise valid objects. They will be
		// used later during deletion.
		var validObjs []*PostgreSQLConfig

		for _, obj := range configs.Items {
			err := obj.Validate()
			if err != nil {
				log.Printf("reconciling loopCnt=%d: error invalid object: %s obj=%#v", reconciliationCnt, err, *obj)
				time.Sleep(reconciliationInterval)
				continue
			}

			validObjs = append(validObjs, obj)

			status, err := processUpdate(ops, obj)
			if err != nil {
				log.Printf("reconciling loopCnt=%d: error: processing update obj=%#v: %s", reconciliationCnt, *obj, err)
			} else {
				log.Printf("reconciling loopCnt=%d: reconciled: %s obj=%#v", reconciliationCnt, status, *obj)
			}
		}

		// We still have to delete databases for custom objects that
		// are gone. This assumes only the operator code does
		// operataions on the database. Databases that still exists
		// but aren't referenced by any custom object are subject of
		// deletion.
		{
			for _, db := range dbs {
				processed := false

				for _, obj := range validObjs {
					if obj.Spec.Database == db.Name {
						processed = true
						break
					}
				}

				if processed {
					time.Sleep(reconciliationInterval)
					continue
				}

				obj := &PostgreSQLConfig{
					Spec: PostgreSQLConfigSpec{
						Database: db.Name,
						Owner:    db.Owner,
					},
				}

				status, err := processDelete(ops, obj)
				if err != nil {
					log.Printf("reconciling loopCnt=%d: error: processing delete obj=%#v: %s", reconciliationCnt, *obj, err)
				} else {
					log.Printf("reconciling loopCnt=%d: reconciled: %s obj=%#v", reconciliationCnt, status, *obj)
				}
			}
		}

		time.Sleep(reconciliationInterval)
	}
}

func processUpdate(ops *postgresqlops.PostgreSQLOps, obj *PostgreSQLConfig) (status string, err error) {
	dbs, err := ops.ListDatabases()
	if err != nil {
		return "", fmt.Errorf("listing databases: %s", err)
	}

	var foundDB postgresqlops.Database
	for _, db := range dbs {
		if db.Name == obj.Spec.Database {
			foundDB = db
			break
		}
	}

	if foundDB.Name == "" {
		err := ops.CreateDatabase(obj.Spec.Database, obj.Spec.Owner)
		if err != nil {
			return "", fmt.Errorf("creating database: %s", err)
		}
		return "database created", nil
	}

	if foundDB.Owner != obj.Spec.Owner {
		err := ops.ChangeDatabaseOwner(obj.Spec.Database, obj.Spec.Owner)
		if err != nil {
			return "", fmt.Errorf("chaning owner=%#q: %s", foundDB.Owner, err)
		}
		return fmt.Sprintf("owner=%#q changed", foundDB.Owner), nil
	}

	return "already reconcilied", nil
}

func processDelete(ops *postgresqlops.PostgreSQLOps, obj *PostgreSQLConfig) (status string, err error) {
	dbs, err := ops.ListDatabases()
	if err != nil {
		return "", fmt.Errorf("listing databases: %s", err)
	}

	var found bool
	for _, db := range dbs {
		if db.Name == obj.Spec.Database {
			found = true
			break
		}
	}

	if found {
		err := ops.DeleteDatabase(obj.Spec.Database)
		if err != nil {
			return "", fmt.Errorf("deleting database: %s", err)
		}
		return "database deleted", nil
	}

	return "already reconcilied", nil
}

func newHttpClient(config Config) (*http.Client, error) {
	crt, err := tls.LoadX509KeyPair(config.K8sCrtFile, config.K8sKeyFile)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	caCert, err := ioutil.ReadFile(config.K8sCAFile)
	if err != nil {
		return nil, err
	}
	certPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{crt},
		RootCAs:      certPool,
	}
	tlsConfig.BuildNameToCertificate()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, nil
}

func readerToBytesTrimSpace(r io.Reader) []byte {
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	b := buf.Bytes()
	b = bytes.TrimSpace(b)
	return b
}

func isStatusAlreadyExists(body []byte) (bool, error) {
	m := make(map[string]interface{})
	err := json.Unmarshal(body, &m)
	if err != nil {
		return false, fmt.Errorf("creating custom resource: %s: decoding body=%s", err, body)
	}
	if m["kind"] != "Status" {
		return false, nil
	}
	return m["reason"] == "AlreadyExists", nil
}
