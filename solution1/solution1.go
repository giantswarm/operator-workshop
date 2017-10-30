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
	"strings"
	"time"

	"github.com/giantswarm/operator-workshop/mysqlops"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("I ")
}

type Config struct {
	K8sServer  string
	K8sCrtFile string
	K8sKeyFile string
	K8sCAFile  string
}

// MySQLConfig is custom object of mysqlconfigs.containerconf.de custom
// resource.
type MySQLConfig struct {
	Spec MySQLConfigSpec `json:"spec"`
}

func (m MySQLConfig) Validate() error {
	if err := m.Spec.Validate(); err != nil {
		return fmt.Errorf("spec is not valid: %s", err)
	}
	return nil
}

// MySQLConfigSpec is custom object specification. Represents the desired state
// towards which the operator reconciles. It also includes information
// necessary to perform the reconciliation, i.e. database access information.
type MySQLConfigSpec struct {
	// Service is service name which points to a MySQL server.
	Service string `json:"service"`
	// Port is the MySQL server listen port.
	Port int `json:"port"`

	// Database is database name to be created.
	Database string `json:"database"`
	// Owner is the database owner.
	Owner string `json:"owner"`
}

func (s MySQLConfigSpec) Validate() error {
	if s.Service == "" {
		return fmt.Errorf("service is not set")
	}
	if s.Port == 0 {
		return fmt.Errorf("port is not set")
	}
	if s.Database == "" {
		return fmt.Errorf("database is not set")
	}
	if s.Owner == "" {
		return fmt.Errorf("owner is not set")
	}
	return nil
}

// MySQLConfigList represents a list of custom objects. It is useful for
// decoding list API calls.
type MySQLConfigList struct {
	Items []*MySQLConfig `json:"items"`
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

	var serverDefault string
	{
		out, err := exec.Command("minikube", "ip").Output()
		if err == nil {
			minikubeIP := strings.TrimSpace(string(out))
			serverDefault = "https://" + string(minikubeIP) + ":8443"
		}
	}

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
				"name": "mysqlconfigs.containerconf.de"
			},
			"spec": {
				"group": "containerconf.de",
				"version": "v1",
				"scope": "Namespaced",
				"names": {
					"plural": "mysqlconfigs",
					"singular": "mysqlconfig",
					"kind": "MySQLConfig"
				},
				"shortNames": []
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
				log.Printf("creating custom resource: created")
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

	// Start reconciliation loop.
	reconciliationCnt := 1
	reconciliationInterval := time.Second * 1
	for ; ; reconciliationCnt++ {
		log.Printf("reconciling loopCnt=%d", reconciliationCnt)

		if ctx.Err() == context.Canceled {
			log.Printf("reconciling loopCnt=%d: context cancelled", reconciliationCnt)
			return nil
		}

		url := config.K8sServer + "/apis/containerconf.de/v1/mysqlconfigs"
		res, err := k8sClient.Get(url)
		if err != nil {
			return fmt.Errorf("reconciling loopCnt=%d: %s", reconciliationCnt, url, err)
		}

		body := readerToBytesTrimSpace(res.Body)
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("reconciling loopCnt=%d: error client response status status=%d body=%#q", reconciliationCnt, res.StatusCode, body)
			continue
		}

		var configs MySQLConfigList
		err = json.Unmarshal(body, &configs)
		if err != nil {
			log.Printf("reconciling loopCnt=%d: error unmarshalling mysqlconfigs list body=%#q: %s", reconciliationCnt, body, err)
			continue
		}

		for _, config := range configs.Items {
			err := config.Validate()
			if err != nil {
				log.Printf("reconciling loopCnt=%d: error invalid mysqlconfig=%#v: %s", reconciliationCnt, *config, err)
				continue
			}

			var ops *mysqlops.MySQLOps
			{
				ops, err = mysqlops.New(mysqlops.Config{})
				if err != nil {
					log.Printf("reconciling loopCnt=%d obj=%#v: error creating MySQLOps: %s", reconciliationCnt, *config, err)
					continue
				}
			}

			// Reconcile MySQLConfig.
			{
				dbs, err := ops.ListDatabases()
				if err != nil {
					log.Printf("reconciling loopCnt=%d obj=%#v: error listing databases: %s", reconciliationCnt, *config, err)
					continue
				}

				var foundDB mysqlops.Database
				for _, db := range dbs {
					if db.Name == config.Spec.Database {
						foundDB = db
						break
					}
				}

				if foundDB.Name != "" {
					db := foundDB

					if db.Owner == config.Spec.Owner {
						log.Printf("reconciling loopCnt=%d obj=%#v: object already reconcilled", reconciliationCnt, *config)
						continue
					}

					log.Printf("reconciling loopCnt=%d obj=%#v: changing owner=%#q", reconciliationCnt, *config, db.Owner)
					err := ops.ChangeDatabaseOwner(config.Spec.Database, config.Spec.Owner)
					if err != nil {
						log.Printf("reconciling loopCnt=%d obj=%#v: changing owner=%#q: error: %s", reconciliationCnt, *config, db.Owner, err)
						continue
					}
					log.Printf("reconciling loopCnt=%d obj=%#v: changing owner=%#q: changed", reconciliationCnt, *config, db.Owner)
				} else {
					log.Printf("reconciling loopCnt=%d obj=%#v: creating database", reconciliationCnt, *config, err)
					err := ops.CreateDatabase(config.Spec.Database, config.Spec.Owner)
					if err != nil {
						log.Printf("reconciling loopCnt=%d obj=%#v: creating database: error: %s", reconciliationCnt, *config, err)
						continue
					}
					log.Printf("reconciling loopCnt=%d obj=%#v: creating database: created", reconciliationCnt, *config)
				}
			}

		}

		log.Printf("reconciling loopCnt=%d: reconciled", reconciliationCnt)
		time.Sleep(reconciliationInterval)
	}

	return nil
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
