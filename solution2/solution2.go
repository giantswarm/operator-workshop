package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"strings"
	"time"

	"github.com/giantswarm/operator-workshop/mysqlops"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apismetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	apismetav1.TypeMeta   `json:",inline"`
	apismetav1.ObjectMeta `json:"metadata,omitempty"`

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
	apismetav1.TypeMeta `json:",inline"`
	apismetav1.ListMeta `json:"metadata,omitempty"`

	Items []*MySQLConfig `json:"items"`
}

// decoder decodes custom objects from a stream. It is used for decoding list
// of objects by client-go watcher.
type decoder struct {
	stream  io.ReadCloser
	jsonDec *json.Decoder
}

func newDecoder(stream io.ReadCloser) *decoder {
	return &decoder{
		stream:  stream,
		jsonDec: json.NewDecoder(stream),
	}
}

func (d *decoder) Decode() (action watch.EventType, object runtime.Object, err error) {
	var e struct {
		Type   watch.EventType
		Object *MySQLConfig
	}
	e.Object = new(MySQLConfig)

	if err := d.jsonDec.Decode(&e); err != nil {
		return watch.Error, nil, err
	}

	return e.Type, e.Object, nil
}

func (d *decoder) Close() {
	d.stream.Close()
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
	k8sClient, err := newK8sExtClient(config)
	if err != nil {
		return fmt.Errorf("creating K8s client: %s", err)
	}

	// Create Custom Resource Definition.
	{
		log.Printf("creating custom resource")

		// crdJson content in YAML format can be found in crd.yaml file.
		crd := &apiextensionsv1beta1.CustomResourceDefinition{
			TypeMeta: apismetav1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1beta1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: apismetav1.ObjectMeta{
				Name: "mysqlconfigs.containerconf.de",
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   "containerconf.de",
				Version: "v1",
				Scope:   apiextensionsv1beta1.NamespaceScoped,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural:     "mysqlconfigs",
					Singular:   "mysqlconfig",
					Kind:       "MySQLConfig",
					ShortNames: []string{},
				},
			},
		}

		_, err := k8sClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
		if apierrors.IsAlreadyExists(err) {
			log.Printf("creating custom resource: already exists")
		} else if err != nil {
			return fmt.Errorf("creating custom resource: %s", err)
		} else {
			log.Printf("creating custom resource: created")
		}
	}

	// Wait for the Custom Resource to be ready.
	{
		attempt := 1
		maxAttempts := 10
		checkInterval := time.Millisecond * 200

		for ; ; attempt++ {
			log.Printf("checking custom resource readiness attempt=%d", attempt)

			_, err := k8sClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get("mysqlconfigs.containerconf.de", apismetav1.GetOptions{})
			if err != nil && attempt == maxAttempts {
				return fmt.Errorf("checking custom resource readiness attempt=%d: %s", attempt, err)
			} else if err != nil {
				log.Printf("checking custom resource readiness attempt=%d: not ready yet", attempt)
				time.Sleep(checkInterval)
			} else {
				log.Printf("checking custom resource readiness attempt=%d: ready", attempt)
				break
			}
		}
	}

	// Start reconciliation loop.

	var watcher watch.Interface
	{
		endpoint := "/apis/containerconf.de/v1/watch/mysqlconfigs"

		restClient := k8sClient.Discovery().RESTClient()

		stream, err := restClient.Get().AbsPath(endpoint).Stream()
		if err != nil {
			return fmt.Errorf("creating a stream for endpoint=%s: %s", endpoint, err)
		}

		watcher = watch.NewStreamWatcher(newDecoder(stream))
		defer watcher.Stop()
	}

	reconciliationCnt := 1
	for ; ; reconciliationCnt++ {
		log.Printf("reconciling loopCnt=%d", reconciliationCnt)

		select {
		case <-ctx.Done():
			log.Printf("reconciling loopCnt=%d: context cancelled", reconciliationCnt)
			return nil
		case event := <-watcher.ResultChan():
			obj, ok := event.Object.(*MySQLConfig)
			if !ok {
				return fmt.Errorf("reconciling loopCnt=%d: wrong type %T, want %T", reconciliationCnt, event.Object, &MySQLConfig{})
			}
			err := obj.Validate()
			if err != nil {
				log.Printf("reconciling loopCnt=%d: error invalid obj=%#v: %s", reconciliationCnt, *obj, err)
				continue
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				status, err := processUpdate(obj)
				if err != nil {
					log.Printf("reconciling loopCnt=%d: error: processing update obj=%#v: %s", reconciliationCnt, *obj, err)
				} else {
					log.Printf("reconciling loopCnt=%d: reconciled: %s obj=%#v", reconciliationCnt, status, *obj)
				}
			case watch.Deleted:
				status, err := processDelete(obj)
				if err != nil {
					log.Printf("reconciling loopCnt=%d: error: processing delete obj=%#v: %s", reconciliationCnt, *obj, err)
				} else {
					log.Printf("reconciling loopCnt=%d: reconciled: %s obj=%#v", reconciliationCnt, status, *obj)
				}
			default:
				log.Printf("reconciling loopCnt=%d: error: unknown event type=%#v, unhandled event=%#v", reconciliationCnt, event.Type, event)
			}
		}
	}

	return nil
}

func processUpdate(obj *MySQLConfig) (status string, err error) {
	var ops *mysqlops.MySQLOps
	{
		ops, err = mysqlops.New(mysqlops.Config{})
		if err != nil {
			return "", fmt.Errorf("creating MySQLOps: %s", err)
		}
	}

	// Reconcile MySQLConfig.
	{
		dbs, err := ops.ListDatabases()
		if err != nil {
			return "", fmt.Errorf("listing databases: %s", err)
		}

		var foundDB mysqlops.Database
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
}

func processDelete(obj *MySQLConfig) (status string, err error) {
	var ops *mysqlops.MySQLOps
	{
		ops, err = mysqlops.New(mysqlops.Config{})
		if err != nil {
			return "", fmt.Errorf("creating MySQLOps: %s", err)
		}
	}

	// Reconcile MySQLConfig.
	{
		dbs, err := ops.ListDatabases()
		if err != nil {
			return "", fmt.Errorf("listing databases: %s", err)
		}

		var foundDB mysqlops.Database
		for _, db := range dbs {
			if db.Name == obj.Spec.Database {
				foundDB = db
				break
			}
		}

		if foundDB.Name != "" {
			err := ops.DeleteDatabase(obj.Spec.Database)
			if err != nil {
				return "", fmt.Errorf("deleting database: %s", err)
			}
			return "database deleted", nil
		}

		return "already reconcilied", nil
	}
}

// newK8sExtClient creates Kubernets extensions API client.
func newK8sExtClient(config Config) (apiextensionsclient.Interface, error) {
	restConfig := &rest.Config{
		Host: config.K8sServer,
		TLSClientConfig: rest.TLSClientConfig{
			CertFile: config.K8sCrtFile,
			KeyFile:  config.K8sKeyFile,
			CAFile:   config.K8sCAFile,
		},
	}

	return apiextensionsclient.NewForConfig(restConfig)
}
