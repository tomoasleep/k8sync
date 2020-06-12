package repository

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/argoproj/gitops-engine/pkg/cache"
	executil "github.com/argoproj/gitops-engine/pkg/utils/exec"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	annotationGCMark = "k8sync/gc-mark"
)

type resourceInfo struct {
	gcMark string
}

// Repository specifies the place of manifests
type Repository struct {
	RepoPath string
	Paths    []string
	Revision string
}

// OpenCurrentDirRepository returns Repository instance at current repository
func OpenCurrentDirRepository() (*Repository, error) {
	repoPath, err := executil.Run(exec.Command("git", "rev-parse", "--show-toplevel"))
	if err != nil {
		return nil, err
	}

	path, err := executil.Run(exec.Command("git", "rev-parse", "--show-prefix"))
	if err != nil {
		return nil, err
	}

	revision, err := executil.Run(exec.Command("git", "rev-parse", "HEAD"))
	if err != nil {
		return nil, err
	}

	return &Repository{
		RepoPath: repoPath,
		Paths:    []string{path},
		Revision: revision,
	}, nil
}

// GetGCMark generate a pruning marker for the key
func (r *Repository) GetGCMark(key kube.ResourceKey) string {
	return fmt.Sprintf("%s/%s", r.RepoPath, strings.Join(r.Paths, ","))
}

// SetGCMark set a pruning marker for the resource
func (r *Repository) SetGCMark(un *unstructured.Unstructured) {
	annotations := un.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationGCMark] = r.GetGCMark(kube.GetResourceKey(un))
	un.SetAnnotations(annotations)
}

// PopulateResourceInfoHandler checks if
func PopulateResourceInfoHandler(un *unstructured.Unstructured, isRoot bool) (interface{}, bool) {
	gcMark := un.GetAnnotations()[annotationGCMark]
	info := &resourceInfo{gcMark: gcMark}

	cacheManifest := gcMark != ""
	return info, cacheManifest
}

// IsManagedResource detects the given resource should be managed
func (r *Repository) IsManagedResource(res *cache.Resource) bool {
	return res.Info.(*resourceInfo).gcMark == r.GetGCMark(res.ResourceKey())
}

// ParseManifests finds resource definitions
func (r *Repository) ParseManifests() ([]*unstructured.Unstructured, error) {
	var res []*unstructured.Unstructured
	for _, path := range r.Paths {
		err := filepath.Walk(filepath.Join(r.RepoPath, path), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			if ext := filepath.Ext(info.Name()); ext != ".yml" && ext != ".yaml" {
				return nil
			}

			data, err := ioutil.ReadFile(path)
			if err != nil {
				return nil
			}

			resources, err := kube.SplitYAML(string(data))
			if err != nil {
				return fmt.Errorf("failed to parse %s:%v", path, err)
			}

			res = append(res, resources...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	for _, resource := range res {
		r.SetGCMark(resource)
	}

	return res, nil
}
