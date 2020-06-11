package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/argoproj/gitops-engine/pkg/cache"
	"github.com/argoproj/gitops-engine/pkg/diff"
	"github.com/argoproj/gitops-engine/pkg/sync"
	"github.com/argoproj/gitops-engine/pkg/sync/common"
	ioutil "github.com/argoproj/gitops-engine/pkg/utils/io"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
)

const (
	operationRefreshTimeout = time.Second * 1
)

// Engine does operation
type Engine interface {
	// Init initializes engine
	Init() (io.Closer, error)
	// Apply resources
	Apply(
		context.Context,
		*Target,
		...sync.SyncOpt,
	) ([]common.ResourceSyncResult, error)
	Plan(*Target) error
}

type engine struct {
	config  *rest.Config
	cache   cache.ClusterCache
	kubectl kube.Kubectl
}

// Target represents expected resources
type Target struct {
	Resources []*unstructured.Unstructured
	IsManaged func(r *cache.Resource) bool
	Revision  string
	Namespace string
}

// NewEngine returns an engine for operation
func NewEngine(config *rest.Config, clusterCache cache.ClusterCache) Engine {
	return &engine{
		config:  config,
		kubectl: &kube.KubectlCmd{},
		cache:   clusterCache,
	}
}

func (e *engine) Init() (io.Closer, error) {
	err := e.cache.EnsureSynced()
	if err != nil {
		return nil, err
	}

	return ioutil.NewCloser(func() error {
		e.cache.Invalidate()
		return nil
	}), nil
}

func (e *engine) Plan(
	target *Target,
) error {
	managedResources, err := e.cache.GetManagedLiveObjs(target.Resources, target.IsManaged)
	if err != nil {
		return err
	}
	result := sync.Reconcile(target.Resources, managedResources, target.Namespace, e.cache)

	if _, ok := os.LookupEnv("KUBECTL_EXTERNAL_DIFF"); !ok {
		os.Setenv("KUBECTL_EXTERNAL_DIFF", "diff -uN")
	}

	for i := range result.Live {
		diff.PrintDiff(result.Target[i].GetName(), result.Live[i], result.Target[i])
	}
	return nil
}

func (e *engine) Apply(
	ctx context.Context,
	target *Target,
	opts ...sync.SyncOpt,
) ([]common.ResourceSyncResult, error) {
	managedResources, err := e.cache.GetManagedLiveObjs(target.Resources, target.IsManaged)
	if err != nil {
		return nil, err
	}
	result := sync.Reconcile(target.Resources, managedResources, target.Namespace, e.cache)

	diffRes, err := diff.DiffArray(result.Target, result.Live, diff.GetNoopNormalizer(), diff.GetDefaultDiffOptions())
	if err != nil {
		return nil, err
	}
	opts = append(opts, sync.WithSkipHooks(!diffRes.Modified))

	logger := log.NewEntry(log.New())
	syncCtx, err := sync.NewSyncContext(target.Revision, result, e.config, e.config, e.kubectl, target.Namespace, logger, opts...)
	if err != nil {
		return nil, err
	}

	for {
		syncCtx.Sync()
		phase, message, resources := syncCtx.GetState()
		if phase.Completed() {
			if phase == common.OperationError {
				err = fmt.Errorf("sync operation failed: %s", message)
			}
			return resources, err
		}

		select {
		case <-ctx.Done():
			syncCtx.Terminate()
			return resources, errors.New("sync operation was terminated")
		case <-time.After(operationRefreshTimeout):
		}
	}
}
