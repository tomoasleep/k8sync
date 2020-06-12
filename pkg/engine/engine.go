package engine

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/argoproj/gitops-engine/pkg/cache"
	"github.com/argoproj/gitops-engine/pkg/diff"
	"github.com/argoproj/gitops-engine/pkg/sync"
	"github.com/argoproj/gitops-engine/pkg/sync/common"
	engineio "github.com/argoproj/gitops-engine/pkg/utils/io"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
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

	return engineio.NewCloser(func() error {
		e.cache.Invalidate()
		return nil
	}), nil
}

func unmarshal(bytes []byte) (*unstructured.Unstructured, error) {
	o := make(map[string]interface{})
	err := yaml.Unmarshal(bytes, &o)
	if err != nil {
		return nil, err
	}

	if o == nil {
		return nil, nil
	}

	un := &unstructured.Unstructured{}
	err = un.UnmarshalJSON(bytes)
	if err != nil {
		return nil, err
	}

	return un, err
}

type Writer struct {
	dir    string
	live   string
	merged string
}

func (w Writer) print(liveBytes []byte, predictedBytes []byte) error {
	live, err := unmarshal(liveBytes)
	if err != nil {
		return err
	}

	predicted, err := unmarshal(predictedBytes)
	if err != nil {
		return err
	}

	var resource *unstructured.Unstructured
	if live != nil {
		resource = live
	} else {
		resource = predicted
	}

	liveYaml, err := yaml.Marshal(live)
	if err != nil {
		return err
	}
	predictedYaml, err := yaml.Marshal(predicted)
	if err != nil {
		return err
	}

	version := resource.GetAPIVersion()
	kind := resource.GetKind()
	namespace := resource.GetNamespace()
	name := resource.GetName()
	key := strings.Replace(fmt.Sprintf("%v.%v.%v.%v", version, kind, namespace, name), "/", ".", -1)

	liveFile := path.Join(w.dir, w.live, key)
	err = ioutil.WriteFile(liveFile, liveYaml, 0644)
	if err != nil {
		return err
	}

	predictedFile := path.Join(w.dir, w.merged, key)
	err = ioutil.WriteFile(predictedFile, predictedYaml, 0644)
	if err != nil {
		return err
	}

	return nil
}

func printDiff(diffRes *diff.DiffResultList) error {
	tempDir, err := ioutil.TempDir("", "k8sync-diff")
	if err != nil {
		return err
	}

	w := &Writer{dir: tempDir, live: "LIVE", merged: "MERGED"}

	if err = os.Mkdir(path.Join(tempDir, w.live), 0777); err != nil {
		return err
	}
	if err = os.Mkdir(path.Join(tempDir, w.merged), 0777); err != nil {
		return err
	}

	for _, diff := range diffRes.Diffs {
		err = w.print(diff.NormalizedLive, diff.PredictedLive)
		if err != nil {
			return err
		}
	}

	cmd := exec.Command("diff", "-u", "-N", w.live, w.merged)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = tempDir

	cmd.Run()

	return nil
}

func (e *engine) Plan(
	target *Target,
) error {
	managedResources, err := e.cache.GetManagedLiveObjs(target.Resources, target.IsManaged)
	if err != nil {
		return err
	}
	result := sync.Reconcile(target.Resources, managedResources, target.Namespace, e.cache)

	diffRes, err := diff.DiffArray(result.Target, result.Live, diff.GetNoopNormalizer(), diff.GetDefaultDiffOptions())
	if err != nil {
		return err
	}

	err = printDiff(diffRes)
	if err != nil {
		return err
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
