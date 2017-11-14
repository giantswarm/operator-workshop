package solution2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/giantswarm/operator-workshop/customobject"
	"github.com/giantswarm/operator-workshop/postgresqlops"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

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

	customobject.PostgreSQLConfigList `json:",inline"`
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
		Object *PostgreSQLConfig
	}
	e.Object = new(PostgreSQLConfig)

	if err := d.jsonDec.Decode(&e); err != nil {
		return watch.Error, nil, err
	}

	return e.Type, e.Object, nil

}

func (d *decoder) Close() {
	d.stream.Close()
}

func Run(ctx context.Context, config Config) error {
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

	// Start reconciliation loop.

	// newWatcherFunc creates a new watcher instance. It is needed as
	// watcher's ResultChan is closed from time to time. In that case
	// watcher is recreated in the reconciliation loop.
	// NOTE: watcher should be stopped to release all resources associated
	// with it.
	newWatcherFunc := func() (watch.Interface, error) {
		endpoint := "/apis/containerconf.de/v1/watch/postgresqlconfigs"

		restClient := k8sClient.Discovery().RESTClient()

		stream, err := restClient.Get().AbsPath(endpoint).Stream()
		if err != nil {
			return nil, fmt.Errorf("creating a stream for endpoint=%s: %s", endpoint, err)
		}

		return watch.NewStreamWatcher(newDecoder(stream)), nil
	}

	watcher, err := newWatcherFunc()
	if err != nil {
		return fmt.Errorf("creating watcher: %s", err)
	}

	for {
		log.Printf("reconciling")

		select {
		case <-ctx.Done():
			log.Printf("reconciling: context cancelled")
			watcher.Stop()
			return nil
		case event, more := <-watcher.ResultChan():
			// When ResultChan is closed stop current watcher and
			// create a new one.
			if !more {
				log.Printf("reconciling: recreating watcher")
				watcher.Stop()
				watcher, err = newWatcherFunc()
				if err != nil {
					return fmt.Errorf("creating watcher: %s", err)
				}
				log.Printf("reconciling: recreating watcher: recreated")
				continue
			}

			var obj *PostgreSQLConfig
			{
				if event.Object == nil {
					obj = nil
				} else {
					var ok bool

					obj, ok = event.Object.(*PostgreSQLConfig)
					if !ok {
						// This error means bug in our
						// code. Decoder is incopatible
						// with the loop implemenation.
						return fmt.Errorf("reconciling: wrong type %T, want %T", event.Object, &PostgreSQLConfig{})
					}
					err := obj.Validate()
					if err != nil {
						log.Printf("reconciling: error invalid obj=%#v: %s", obj.PostgreSQLConfig, err)
						continue
					}
				}
			}

			switch event.Type {
			// In Giant Swarm we believe that you should treat
			// Added and Modified (or created and updated) as the
			// same thing. Otherwise you most likely don't write
			// a correct reconciliation.
			case watch.Added, watch.Modified:
				status, err := resource.EnsureCreated(&obj.PostgreSQLConfig)
				if err != nil {
					log.Printf("reconciling: error: processing update obj=%#v: %s", obj.PostgreSQLConfig, err)
				} else {
					log.Printf("reconciling: reconciled: %s obj=%#v", status, obj.PostgreSQLConfig)
				}
			case watch.Deleted:
				status, err := resource.EnsureDeleted(&obj.PostgreSQLConfig)
				if err != nil {
					log.Printf("reconciling: error: processing delete obj=%#v: %s", obj.PostgreSQLConfig, err)
				} else {
					log.Printf("reconciling: reconciled: %s obj=%#v", status, obj.PostgreSQLConfig)
				}
			case watch.Error:
				log.Printf("reconciling: error: event=%#v", event)
			default:
				log.Printf("reconciling: error: unknown event type=%#v, unhandled event=%#v", event.Type, event)
			}
		}
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
