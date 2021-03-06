package metricsresource

import (
	"context"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/operatorkit/framework"
)

const (
	// Name is the identifier of the resource.
	Name = "metrics"
)

// Config represents the configuration used to create a new metrics resource.
type Config struct {
	// Dependencies.
	Resource framework.Resource

	// Settings.

	// Name is name of the service using the reconciler framework. This may be the
	// name of the executing operator or controller. The service name will be used
	// to label metrics.
	Name string
}

// DefaultConfig provides a default configuration to create a new metrics
// resource by best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		Resource: nil,

		// Settings.
		Name: "",
	}
}

type Resource struct {
	// Dependencies.
	resource framework.Resource

	// Settings.
	name string
}

// New creates a new configured metrics resource.
func New(config Config) (*Resource, error) {
	// Dependencies.
	if config.Resource == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Resource must not be empty")
	}

	// Settings.
	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Name must not be empty")
	}

	newResource := &Resource{
		// Dependencies.
		resource: config.Resource,

		// Settings.
		name: toCamelCase(config.Name),
	}

	return newResource, nil
}

func (r *Resource) GetCurrentState(ctx context.Context, obj interface{}) (interface{}, error) {
	o := "GetCurrentState"

	defer r.updateMetrics(o, time.Now())

	v, err := r.resource.GetCurrentState(ctx, obj)
	if err != nil {
		r.updateErrorMetrics(o)
		return nil, microerror.Mask(err)
	}

	return v, nil
}

func (r *Resource) GetDesiredState(ctx context.Context, obj interface{}) (interface{}, error) {
	o := "GetDesiredState"

	defer r.updateMetrics(o, time.Now())

	v, err := r.resource.GetDesiredState(ctx, obj)
	if err != nil {
		r.updateErrorMetrics(o)
		return nil, microerror.Mask(err)
	}

	return v, nil
}

func (r *Resource) NewUpdatePatch(ctx context.Context, obj, currentState, desiredState interface{}) (*framework.Patch, error) {
	o := "NewUpdatePatch"

	defer r.updateMetrics(o, time.Now())

	v, err := r.resource.NewUpdatePatch(ctx, obj, currentState, desiredState)
	if err != nil {
		r.updateErrorMetrics(o)
		return nil, microerror.Mask(err)
	}

	return v, nil
}

func (r *Resource) NewDeletePatch(ctx context.Context, obj, currentState, desiredState interface{}) (*framework.Patch, error) {
	o := "NewDeletePatch"

	defer r.updateMetrics(o, time.Now())

	v, err := r.resource.NewDeletePatch(ctx, obj, currentState, desiredState)
	if err != nil {
		r.updateErrorMetrics(o)
		return nil, microerror.Mask(err)
	}

	return v, nil
}

func (r *Resource) Name() string {
	return Name
}

func (r *Resource) ApplyCreateChange(ctx context.Context, obj, createState interface{}) error {
	o := "ApplyCreatePatch"

	defer r.updateMetrics(o, time.Now())

	err := r.resource.ApplyCreateChange(ctx, obj, createState)
	if err != nil {
		r.updateErrorMetrics(o)
		return microerror.Mask(err)
	}

	return nil
}

func (r *Resource) ApplyDeleteChange(ctx context.Context, obj, deleteState interface{}) error {
	o := "ApplyDeletePatch"

	defer r.updateMetrics(o, time.Now())

	err := r.resource.ApplyDeleteChange(ctx, obj, deleteState)
	if err != nil {
		r.updateErrorMetrics(o)
		return microerror.Mask(err)
	}

	return nil
}

func (r *Resource) ApplyUpdateChange(ctx context.Context, obj, updateState interface{}) error {
	o := "ApplyUpdatePatch"

	defer r.updateMetrics(o, time.Now())

	err := r.resource.ApplyUpdateChange(ctx, obj, updateState)
	if err != nil {
		r.updateErrorMetrics(o)
		return microerror.Mask(err)
	}

	return nil
}

func (r *Resource) Underlying() framework.Resource {
	return r.resource.Underlying()
}

func (r *Resource) updateErrorMetrics(operation string) {
	errorTotal.WithLabelValues(r.name, r.resource.Underlying().Name(), operation).Inc()
}

func (r *Resource) updateMetrics(operation string, startTime time.Time) {
	operationDuration.WithLabelValues(r.name, r.resource.Underlying().Name(), operation).Set(float64(time.Since(startTime) / time.Millisecond))
	operationTotal.WithLabelValues(r.name, r.resource.Underlying().Name(), operation).Inc()
}
