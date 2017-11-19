package solution3

import (
	"context"
	"fmt"
	"log"

	"github.com/cenk/backoff"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/operator-workshop/customobject"
	"github.com/giantswarm/operator-workshop/postgresqlops"
	"github.com/giantswarm/operatorkit/client/k8sextclient"
	operatorkitcrd "github.com/giantswarm/operatorkit/crd"
	"github.com/giantswarm/operatorkit/crdclient"
	operatorkitinformer "github.com/giantswarm/operatorkit/informer"
	"k8s.io/apimachinery/pkg/runtime"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apismetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// PostgreSQLConfig embeds customobject.PostgreSQLConfig adding fields required
// by runtime.Object interface.
type PostgreSQLConfig struct {
	apismetav1.TypeMeta   `json:",inline"`
	apismetav1.ObjectMeta `json:"metadata,omitempty"`

	customobject.PostgreSQLConfig `json:",inline"`
}

// PostgreSQLConfigList embeds customobject.PostgreSQLConfigList adding fields
// required by runtime.Object interface.
type PostgreSQLConfigList struct {
	apismetav1.TypeMeta `json:",inline"`
	apismetav1.ListMeta `json:"metadata,omitempty"`

	Items []*PostgreSQLConfig `json:"items"`
}

func Run(ctx context.Context, config Config) error {
	var err error

	var logger micrologger.Logger
	{
		c := micrologger.DefaultConfig()
		logger, err = micrologger.New(c)
		if err != nil {
			return fmt.Errorf("creating logger: %s", err)
		}
	}

	var k8sClient apiextensionsclient.Interface
	{
		c := k8sextclient.DefaultConfig()
		c.Logger = logger
		c.InCluster = config.K8sInCluster
		c.Address = config.K8sServer
		c.TLS.CrtFile = config.K8sCrtFile
		c.TLS.CAFile = config.K8sCAFile
		c.TLS.KeyFile = config.K8sKeyFile
		k8sClient, err = k8sextclient.New(c)
		if err != nil {
			return fmt.Errorf("creating k8s api extensions client: %s", err)
		}
	}

	var crd *operatorkitcrd.CRD
	{
		c := operatorkitcrd.DefaultConfig()
		c.Group = "containerconf.de"
		c.Kind = "PostgreSQLConfig"
		c.Version = "v1"
		c.Name = "postgresqlconfigs.containerconf.de"
		c.Plural = "postgresqlconfigs"
		c.Singular = "postgresqlconfig"
		c.Scope = "Namespaced"
		crd, err = operatorkitcrd.New(c)
		if err != nil {
			return fmt.Errorf("creating operatorkit/crd: %s", err)
		}
	}

	var crdClient *crdclient.CRDClient
	{
		c := crdclient.DefaultConfig()
		c.Logger = logger
		c.K8sExtClient = k8sClient
		crdClient, err = crdclient.New(c)
		if err != nil {
			return fmt.Errorf("creating CRDClient: %s", err)
		}
	}

	// Create Custom Resource Definition.
	{
		log.Printf("creating custom resource")
		backOff := backoff.WithMaxTries(backoff.NewExponentialBackOff(), 10)
		err := crdClient.Ensure(ctx, crd, backOff)
		if err != nil {
			return fmt.Errorf("creating custom resource: %s", err)
		}
		log.Printf("creating custom resource: created")
	}

	// Create an informer.
	var informer *operatorkitinformer.Informer
	{
		zeroObjectFactory := operatorkitinformer.ZeroObjectFactoryFuncs{
			NewObjectFunc:     func() runtime.Object { return new(PostgreSQLConfig) },
			NewObjectListFunc: func() runtime.Object { return new(PostgreSQLConfigList) },
		}

		watcherFactory := operatorkitinformer.NewWatcherFactory(
			k8sClient.Apiextensions().RESTClient(),
			crd.WatchEndpoint(),
			zeroObjectFactory,
		)

		c := operatorkitinformer.DefaultConfig()
		c.BackOff = backoff.WithMaxTries(backoff.NewExponentialBackOff(), 10)
		c.WatcherFactory = watcherFactory

		informer, err = operatorkitinformer.New(c)
		if err != nil {
			log.Printf("creating informer: %s", err)
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

	// Create reconciliation events handler functions.

	onUpdateFunc := func(obj interface{}) {
		postgreSQLConfig, ok := obj.(*PostgreSQLConfig)
		if !ok {
			log.Printf("reconciling: wrong type %T, want %T", obj, postgreSQLConfig)
		}
		err := customobject.Validate(postgreSQLConfig.PostgreSQLConfig)
		if err != nil {
			log.Printf("reconciling: error invalid obj=%#v: %s", postgreSQLConfig.PostgreSQLConfig, err)
		}

		status, err := resource.EnsureCreated(&postgreSQLConfig.PostgreSQLConfig)
		if err != nil {
			log.Printf("reconciling: error: processing update obj=%#v: %s", postgreSQLConfig.PostgreSQLConfig, err)
		} else {
			log.Printf("reconciling: reconciled: %s obj=%#v", status, postgreSQLConfig.PostgreSQLConfig)
		}
	}

	onDeleteFunc := func(obj interface{}) {
		postgreSQLConfig, ok := obj.(*PostgreSQLConfig)
		if !ok {
			log.Printf("reconciling: wrong type %T, want %T", obj, postgreSQLConfig)
		}
		err := customobject.Validate(postgreSQLConfig.PostgreSQLConfig)
		if err != nil {
			log.Printf("reconciling: error invalid obj=%#v: %s", postgreSQLConfig.PostgreSQLConfig, err)
		}

		status, err := resource.EnsureDeleted(&postgreSQLConfig.PostgreSQLConfig)
		if err != nil {
			log.Printf("reconciling: error: processing delete obj=%#v: %s", postgreSQLConfig.PostgreSQLConfig, err)
		} else {
			log.Printf("reconciling: reconciled: %s obj=%#v", status, postgreSQLConfig.PostgreSQLConfig)
		}
	}

	// Start reconciliation loop.

	// In Giant Swarm we believe that you should treat Added and Updated as
	// the same thing. Otherwise you most likely don't write a correct
	// reconciliation.
	deleteChan, updateChan, errChan := informer.Watch(ctx)

	for {
		select {
		case event := <-deleteChan:
			onDeleteFunc(event.Object)
		case event := <-updateChan:
			onUpdateFunc(event.Object)
		case err := <-errChan:
			return fmt.Errorf("reconciling: informer error: %s", err)
		}
	}
}
