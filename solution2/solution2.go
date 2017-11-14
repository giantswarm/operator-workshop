package solution2

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/giantswarm/operator-workshop/customobject"
	"github.com/giantswarm/operator-workshop/postgresqlops"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	k8sClient, err := newK8sExtClient(config)
	if err != nil {
		return fmt.Errorf("creating K8s client: %s", err)
	}
	k8sCustomRestClient, err := newK8sCustomRestClient(config, "containerconf.de", "v1")
	if err != nil {
		return fmt.Errorf("creating K8s custom REST client: %s", err)
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
				Name: "postgresqlconfigs.containerconf.de",
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   "containerconf.de",
				Version: "v1",
				Scope:   apiextensionsv1beta1.NamespaceScoped,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural:     "postgresqlconfigs",
					Singular:   "postgresqlconfig",
					Kind:       "PostgreSQLConfig",
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

			_, err := k8sClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get("postgresqlconfigs.containerconf.de", apismetav1.GetOptions{})
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
		err := postgreSQLConfig.Validate()
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
		err := postgreSQLConfig.Validate()
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
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { onUpdateFunc(obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { onUpdateFunc(newObj) },
		DeleteFunc: func(obj interface{}) { onDeleteFunc(obj) },
	}

	listWatch := cache.NewListWatchFromClient(k8sCustomRestClient, "postgresqlconfigs", "", fields.Everything())

	_, controller := cache.NewInformer(listWatch, &PostgreSQLConfig{}, time.Second*15, handler)

	controller.Run(ctx.Done())

	return nil
}

// newK8sExtClient creates Kubernets extensions API client.
func newK8sExtClient(config Config) (apiextensionsclient.Interface, error) {
	restConfig, err := newBaseRestConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating REST config: %s", err)
	}

	return apiextensionsclient.NewForConfig(restConfig)
}

func newK8sCustomRestClient(config Config, group, version string) (rest.Interface, error) {
	restConfig, err := newBaseRestConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating REST config: %s", err)
	}

	var groupVersion schema.GroupVersion
	{
		groupVersion = schema.GroupVersion{
			Group:   group,
			Version: version,
		}
	}

	var scheme *runtime.Scheme
	{
		scheme = runtime.NewScheme()
		scheme.AddKnownTypes(
			groupVersion,
			&PostgreSQLConfig{},
			&PostgreSQLConfigList{},
		)
		apismetav1.AddToGroupVersion(scheme, groupVersion)
	}

	restConfig.GroupVersion = &groupVersion
	restConfig.APIPath = "/apis"
	restConfig.ContentType = runtime.ContentTypeJSON
	restConfig.NegotiatedSerializer = serializer.DirectCodecFactory{
		CodecFactory: serializer.NewCodecFactory(scheme),
	}

	return rest.RESTClientFor(restConfig)
}

func newBaseRestConfig(config Config) (*rest.Config, error) {
	var restConfig *rest.Config

	if config.K8sInCluster {
		var err error
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("creating incluster config: %s", err)
		}
	} else {
		restConfig = &rest.Config{
			Host: config.K8sServer,
			TLSClientConfig: rest.TLSClientConfig{
				CertFile: config.K8sCrtFile,
				KeyFile:  config.K8sKeyFile,
				CAFile:   config.K8sCAFile,
			},
		}
	}

	return restConfig, nil
}
