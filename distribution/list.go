package distribution

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/registry"
)

type byTagName []*types.RepositoryTag

func (r byTagName) Len() int           { return len(r) }
func (r byTagName) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r byTagName) Less(i, j int) bool { return r[i].Tag < r[j].Tag }

type byAPIVersion []registry.APIEndpoint

func (r byAPIVersion) Len() int      { return len(r) }
func (r byAPIVersion) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r byAPIVersion) Less(i, j int) bool {
	if r[i].Version < r[j].Version {
		return true
	}
	if r[i].Version == r[j].Version && strings.HasPrefix(r[i].URL, "https://") && !strings.HasPrefix(r[j].URL, "https://") {
		return true
	}
	return false
}

// TagLister allows to list tags of remote repository.
type TagLister interface {
	ListTags() (tagList []*types.RepositoryTag, fallback bool, err error)
}

// ListRemoteTagsConfig allows to specify transport paramater for remote ta listing.
type ListRemoteTagsConfig struct {
	// MetaHeaders stores HTTP headers with metadata about the image
	// (DockerHeaders with prefix X-Meta- in the request).
	MetaHeaders map[string][]string
	// AuthConfig holds authentication credentials for authenticating with
	// the registry.
	AuthConfig *types.AuthConfig
	// OutStream is the output writer for showing the status of the pull
	// operation.
	OutStream io.Writer
	// RegistryService is the registry service to use for TLS configuration
	// and endpoint lookup.
	RegistryService *registry.Service
}

// ListRemoteTags fetches a tag list from remote repository
func ListRemoteTags(ref reference.Named, config *ListRemoteTagsConfig) (*types.RepositoryTagList, error) {
	var tagList *types.RepositoryTagList
	// Unless the index name is specified, iterate over all registries until
	// the matching image is found.
	if registry.IsReferenceFullyQualified(ref) {
		return getRemoteTagList(ref, config)
	}
	if len(registry.RegistryList) == 0 {
		return nil, fmt.Errorf("No configured registry to pull from.")
	}
	err := registry.ValidateRepositoryName(ref)
	if err != nil {
		return nil, err
	}
	for _, r := range registry.RegistryList {
		// Prepend the index name to the image name.
		fqr, _err := registry.FullyQualifyReferenceWith(r, ref)
		if _err != nil {
			logrus.Warnf("Failed to fully qualify %q name with %q registry: %v", ref.Name(), r, _err)
			err = _err
			continue
		}
		if tagList, err = getRemoteTagList(fqr, config); err == nil {
			return tagList, nil
		}
	}
	return tagList, err
}

// newTagLister creates a specific tag lister for given endpoint.
func newTagLister(endpoint registry.APIEndpoint, repoInfo *registry.RepositoryInfo, config *ListRemoteTagsConfig) (TagLister, error) {
	switch endpoint.Version {
	case registry.APIVersion2:
		return &v2TagLister{
			endpoint: endpoint,
			config:   config,
			repoInfo: repoInfo,
		}, nil
	case registry.APIVersion1:
		return &v1TagLister{
			endpoint: endpoint,
			config:   config,
			repoInfo: repoInfo,
		}, nil
	}
	return nil, fmt.Errorf("unknown version %d for registry %s", endpoint.Version, endpoint.URL)
}

func getRemoteTagList(ref reference.Named, config *ListRemoteTagsConfig) (*types.RepositoryTagList, error) {
	// Resolve the Repository name from fqn to RepositoryInfo
	repoInfo, err := config.RegistryService.ResolveRepository(ref)
	if err != nil {
		return nil, err
	}

	if err := validateRepoName(repoInfo.LocalName.Name()); err != nil {
		return nil, err
	}

	endpoints, err := config.RegistryService.LookupPullEndpoints(repoInfo.CanonicalName)
	if err != nil {
		return nil, err
	}
	// Prefer v1 versions which provide also image ids
	sort.Sort(byAPIVersion(endpoints))

	var (
		lastErr error
		// discardNoSupportErrors is used to track whether an endpoint encountered an error of type registry.ErrNoSupport
		// By default it is false, which means that if a ErrNoSupport error is encountered, it will be saved in lastErr.
		// As soon as another kind of error is encountered, discardNoSupportErrors is set to true, avoiding the saving of
		// any subsequent ErrNoSupport errors in lastErr.
		// It's needed for pull-by-digest on v1 endpoints: if there are only v1 endpoints configured, the error should be
		// returned and displayed, but if there was a v2 endpoint which supports pull-by-digest, then the last relevant
		// error is the ones from v2 endpoints not v1.
		discardNoSupportErrors bool
		tagList                = &types.RepositoryTagList{Name: repoInfo.CanonicalName.Name()}
	)
	for _, endpoint := range endpoints {
		logrus.Debugf("Trying to fetch tag list of %s repository from %s %s", repoInfo.CanonicalName.String(), endpoint.URL, endpoint.Version)
		fallback := false

		tagLister, err := newTagLister(endpoint, repoInfo, config)
		if err != nil {
			lastErr = err
			continue
		}
		tagList.TagList, fallback, err = tagLister.ListTags()
		if err != nil {
			// We're querying v1 registries first. Let's ignore errors until
			// the first v2 registry.
			if fallback || endpoint.Version == registry.APIVersion1 {
				if _, ok := err.(registry.ErrNoSupport); !ok {
					// Because we found an error that's not ErrNoSupport, discard all subsequent ErrNoSupport errors.
					discardNoSupportErrors = true
					// save the current error
					lastErr = err
				} else if !discardNoSupportErrors {
					// Save the ErrNoSupport error, because it's either the first error or all encountered errors
					// were also ErrNoSupport errors.
					lastErr = err
				}
				continue
			}
			logrus.Debugf("Not continuing with error: %v", err)
			return nil, err
		}

		sort.Sort(byTagName(tagList.TagList))
		return tagList, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no endpoints found for %s", repoInfo.Index.Name)
	}
	return nil, lastErr
}
