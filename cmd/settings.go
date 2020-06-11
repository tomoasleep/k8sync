package cmd

import (
	"github.com/argoproj/gitops-engine/pkg/cache"
	"github.com/argoproj/gitops-engine/pkg/sync"
	"github.com/tomoasleep/k8sync/pkg/engine"
	"github.com/tomoasleep/k8sync/pkg/repository"
	"k8s.io/client-go/tools/clientcmd"
)

type settings struct {
	repository   *repository.Repository
	clientConfig clientcmd.ClientConfig

	namespace string

	prune  bool
	dryRun bool
}

func (s *settings) ApplySyncSettings() sync.SyncOpt {
	return sync.WithOperationSettings(s.dryRun, s.prune, false, false)
}

func (s *settings) GetTarget() (*engine.Target, error) {
	targetResources, err := s.repository.ParseManifests()
	if err != nil {
		return nil, err
	}

	return &engine.Target{
		Resources: targetResources,
		IsManaged: s.repository.IsManagedResource,
		Revision:  s.repository.Revision,
		Namespace: s.namespace,
	}, nil
}

func (s *settings) newEngine() (engine.Engine, error) {
	config, err := s.clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	namespace, _, err := s.clientConfig.Namespace()
	if err != nil {
		return nil, err
	}

	clusterCache := cache.NewClusterCache(
		config,
		cache.SetNamespaces([]string{namespace}),
		cache.SetPopulateResourceInfoHandler(repository.PopulateResourceInfoHandler),
	)

	return engine.NewEngine(config, clusterCache), nil
}

func newSettings(clientConfig clientcmd.ClientConfig, prune bool, dryRun bool) (*settings, error) {
	repo, err := repository.OpenCurrentDirRepository()
	if err != nil {
		return nil, err
	}

	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return nil, err
	}

	return &settings{
		repository:   repo,
		dryRun:       dryRun,
		prune:        prune,
		clientConfig: clientConfig,
		namespace:    namespace,
	}, nil
}
