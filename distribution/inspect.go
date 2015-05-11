package distribution

import (
	"fmt"
	"io"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/distribution/metadata"
	"github.com/docker/docker/image"
	"github.com/docker/docker/registry"
)

// InspectConfig allows you to pass transport-related data to Inspect
// function.
type InspectConfig struct {
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
	// MetadataStore is the storage backend for distribution-specific
	// metadata.
	MetadataStore metadata.Store
}

// ManifestFetcher allows to pull image's json without any binary blobs.
type ManifestFetcher interface {
	Fetch(ref reference.Named) (imgInspect *types.RemoteImageInspect, fallback bool, err error)
}

// NewManifestFetcher creates appropriate fetcher instance for given endpoint.
func newManifestFetcher(endpoint registry.APIEndpoint, repoInfo *registry.RepositoryInfo, config *InspectConfig) (ManifestFetcher, error) {
	switch endpoint.Version {
	case registry.APIVersion2:
		return &v2ManifestFetcher{
			endpoint: endpoint,
			config:   config,
			repoInfo: repoInfo,
		}, nil
	case registry.APIVersion1:
		return &v1ManifestFetcher{
			endpoint: endpoint,
			config:   config,
			repoInfo: repoInfo,
		}, nil
	}
	return nil, fmt.Errorf("unknown version %d for registry %s", endpoint.Version, endpoint.URL)
}

func makeRemoteImageInspect(repoInfo *registry.RepositoryInfo, img *image.Image, tag string, dgst digest.Digest) *types.RemoteImageInspect {
	var repoTags = make([]string, 0, 1)
	if tag != "" {
		tagged, err := reference.WithTag(repoInfo.CanonicalName, tag)
		if err == nil {
			repoTags = append(repoTags, tagged.String())
		}
	}
	var repoDigests = make([]string, 0, 1)
	if err := dgst.Validate(); err == nil {
		repoDigests = append(repoDigests, dgst.String())
	}
	return &types.RemoteImageInspect{
		ImageInspectBase: types.ImageInspectBase{
			ID:              img.ID().String(),
			RepoTags:        repoTags,
			RepoDigests:     repoDigests,
			Parent:          img.Parent.String(),
			Comment:         img.Comment,
			Created:         img.Created.Format(time.RFC3339Nano),
			Container:       img.Container,
			ContainerConfig: &img.ContainerConfig,
			DockerVersion:   img.DockerVersion,
			Author:          img.Author,
			Config:          img.Config,
			Architecture:    img.Architecture,
			Os:              img.OS,
			Size:            img.Size,
		},
		Registry: repoInfo.Index.Name,
	}
}

// Inspect returns metadata for remote image.
func Inspect(ref reference.Named, config *InspectConfig) (*types.RemoteImageInspect, error) {
	var (
		imageInspect *types.RemoteImageInspect
		err          error
	)
	// Unless the index name is specified, iterate over all registries until
	// the matching image is found.
	if registry.IsReferenceFullyQualified(ref) {
		return fetchManifest(ref, config)
	}
	if len(registry.RegistryList) == 0 {
		return nil, fmt.Errorf("No configured registry to pull from.")
	}
	for _, r := range registry.RegistryList {
		// Prepend the index name to the image name.
		fqr, _err := registry.FullyQualifyReferenceWith(r, ref)
		if _err != nil {
			logrus.Warnf("Failed to fully qualify %q name with %q registry: %v", ref.Name(), r, _err)
			err = _err
			continue
		}
		// Prepend the index name to the image name.
		if imageInspect, err = fetchManifest(fqr, config); err == nil {
			return imageInspect, nil
		}
	}
	return imageInspect, err
}

func fetchManifest(ref reference.Named, config *InspectConfig) (*types.RemoteImageInspect, error) {
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
		imgInspect             *types.RemoteImageInspect
	)
	for _, endpoint := range endpoints {
		logrus.Debugf("Trying to fetch image manifest of %s repository from %s %s", repoInfo.CanonicalName, endpoint.URL, endpoint.Version)
		fallback := false

		fetcher, err := newManifestFetcher(endpoint, repoInfo, config)
		if err != nil {
			lastErr = err
			continue
		}
		imgInspect, fallback, err = fetcher.Fetch(ref)
		if err != nil {
			if fallback {
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

		return imgInspect, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no endpoints found for %s", repoInfo.Index.Name)
	}
	return nil, lastErr
}
