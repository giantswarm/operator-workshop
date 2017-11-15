package solution1

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/giantswarm/operator-workshop/customobject"
	"github.com/giantswarm/operator-workshop/postgresqlops"
)

type Config struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string

	K8sInCluster bool
	K8sServer    string
	K8sCrtFile   string
	K8sKeyFile   string
	K8sCAFile    string
}

type PostgreSQLConfigList struct {
	Items []*customobject.PostgreSQLConfig `json:"items"`
}

func Run(ctx context.Context, config Config) error {
	if config.K8sInCluster {
		return fmt.Errorf("incluster mode is not supported in solution1")
	}

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

			url := config.K8sServer + "/apis/containerconf.de/v1/postgresqlconfigs"
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

	// Create a resource instance providing reconciliation methods.
	var resource *customobject.Resource
	{
		resource = customobject.NewResource(ops)
	}

	// Start reconciliation loop. In every iteration the operator lists
	// current custom objects and reconciles towards the state described in
	// them. The loop is inifinite, can be cancelled with cancelling the
	// context.
	reconciliationInterval := time.Second * 2
	for {
		log.Printf("reconciling")

		if ctx.Err() == context.Canceled {
			log.Printf("reconciling: context cancelled")
			return nil
		}

		url := config.K8sServer + "/apis/containerconf.de/v1/postgresqlconfigs"
		res, err := k8sClient.Get(url)
		if err != nil {
			return fmt.Errorf("reconciling: requesting url=%#q: %s", url, err)
		}

		body := readerToBytesTrimSpace(res.Body)
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("reconciling: error client response status status=%d body=%#q", res.StatusCode, body)
			time.Sleep(reconciliationInterval)
			continue
		}

		var configs customobject.PostgreSQLConfigList
		err = json.Unmarshal(body, &configs)
		if err != nil {
			log.Printf("reconciling: error unmarshalling postgresqlconfigs list: %s body=%#q", err, body)
			time.Sleep(reconciliationInterval)
			continue
		}

		// Many DB operations are repeated. This can be
		// optimised but it isn't really an issue.
		dbs, err := ops.ListDatabases()
		if err != nil {
			log.Printf("reconciling: error listing databases: %s", err)
			time.Sleep(reconciliationInterval)
			continue
		}

		// Reconcile updates and memorise valid objects. They will be
		// used later during deletion.
		var validObjs []*customobject.PostgreSQLConfig

		for _, obj := range configs.Items {
			err := customobject.Validate(*obj)
			if err != nil {
				log.Printf("reconciling: error invalid object: %s obj=%#v", err, *obj)
				continue
			}

			validObjs = append(validObjs, obj)

			status, err := resource.EnsureCreated(obj)
			if err != nil {
				log.Printf("reconciling: error: processing update obj=%#v: %s", *obj, err)
			} else {
				log.Printf("reconciling: reconciled: %s obj=%#v", status, *obj)
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
					continue
				}

				obj := &customobject.PostgreSQLConfig{
					Spec: customobject.PostgreSQLConfigSpec{
						Database: db.Name,
						Owner:    db.Owner,
					},
				}

				status, err := resource.EnsureDeleted(obj)
				if err != nil {
					log.Printf("reconciling: error: processing delete obj=%#v: %s", *obj, err)
				} else {
					log.Printf("reconciling: reconciled: %s obj=%#v", status, *obj)
				}
			}
		}

		time.Sleep(reconciliationInterval)
	}
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
