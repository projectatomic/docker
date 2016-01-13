package distribution

import (
	"fmt"
	"io"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/digest"
	"github.com/docker/docker/distribution/metadata"
	"github.com/docker/docker/image"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
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
	Fetch(ctx context.Context, ref reference.Named) (imgInspect *types.RemoteImageInspect, err error)
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
	if tagged, isTagged := repoInfo.Named.(reference.NamedTagged); isTagged || tag != "" {
		if !isTagged {
			newTagged, err := reference.WithTag(repoInfo, tag)
			if err == nil {
				tagged = newTagged
			}
		}
		if tagged != nil {
			repoTags = append(repoTags, tagged.String())
		}
	}

	var repoDigests = make([]string, 0, 1)
	if err := dgst.Validate(); err == nil {
		repoDigests = append(repoDigests, dgst.String())
	}

	return &types.RemoteImageInspect{
		V1ID: img.V1Image.ID,
		ImageInspectBase: types.ImageInspectBase{
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
func Inspect(ctx context.Context, ref reference.Named, config *InspectConfig) (*types.RemoteImageInspect, error) {
	var imageInspect *types.RemoteImageInspect
	// Unless the index name is specified, iterate over all registries until
	// the matching image is found.
	if reference.IsReferenceFullyQualified(ref) {
		return fetchManifest(ctx, ref, config)
	}
	if len(registry.DefaultRegistries) == 0 {
		return nil, fmt.Errorf("No configured registry to pull from.")
	}
	err := validateRepoName(ref.Name())
	if err != nil {
		return nil, err
	}
	for _, r := range registry.DefaultRegistries {
		// Prepend the index name to the image name.
		fqr, _err := reference.QualifyUnqualifiedReference(ref, r)
		if _err != nil {
			logrus.Warnf("Failed to fully qualify %q name with %q registry: %v", ref.Name(), r, _err)
			err = _err
			continue
		}
		// Prepend the index name to the image name.
		if imageInspect, err = fetchManifest(ctx, fqr, config); err == nil {
			return imageInspect, nil
		}
	}
	return imageInspect, err
}

func fetchManifest(ctx context.Context, ref reference.Named, config *InspectConfig) (*types.RemoteImageInspect, error) {
	// Resolve the Repository name from fqn to RepositoryInfo
	repoInfo, err := config.RegistryService.ResolveRepository(ref)
	if err != nil {
		return nil, err
	}

	if err := validateRepoName(repoInfo.Name()); err != nil {
		return nil, err
	}

	endpoints, err := config.RegistryService.LookupPullEndpoints(repoInfo)
	if err != nil {
		return nil, err
	}

	var (
		errors []error
		// discardNoSupportErrors is used to track whether an endpoint encountered an error of type registry.ErrNoSupport
		// By default it is false, which means that if a ErrNoSupport error is encountered, it will be saved in lastErr.
		// As soon as another kind of error is encountered, discardNoSupportErrors is set to true, avoiding the saving of
		// any subsequent ErrNoSupport errors in lastErr.
		// It's needed for pull-by-digest on v1 endpoints: if there are only v1 endpoints configured, the error should be
		// returned and displayed, but if there was a v2 endpoint which supports pull-by-digest, then the last relevant
		// error is the ones from v2 endpoints not v1.
		discardNoSupportErrors bool
		imgInspect             *types.RemoteImageInspect

		// confirmedV2 is set to true if a pull attempt managed to
		// confirm that it was talking to a v2 registry. This will
		// prevent fallback to the v1 protocol.
		confirmedV2 bool
	)
	for _, endpoint := range endpoints {
		if confirmedV2 && endpoint.Version == registry.APIVersion1 {
			logrus.Debugf("Skipping v1 endpoint %s because v2 registry was detected", endpoint.URL)
			continue
		}
		logrus.Debugf("Trying to fetch image manifest of %s repository from %s %s", repoInfo.Name(), endpoint.URL, endpoint.Version)

		fetcher, err := newManifestFetcher(endpoint, repoInfo, config)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		if imgInspect, err = fetcher.Fetch(ctx, ref); err != nil {
			// Was this fetch cancelled? If so, don't try to fall back.
			fallback := false
			select {
			case <-ctx.Done():
			default:
				if fallbackErr, ok := err.(fallbackError); ok {
					fallback = true
					confirmedV2 = confirmedV2 || fallbackErr.confirmedV2
					err = fallbackErr.err
				}
			}
			if fallback {
				if _, ok := err.(registry.ErrNoSupport); !ok {
					// Because we found an error that's not ErrNoSupport, discard all subsequent ErrNoSupport errors.
					discardNoSupportErrors = true
					// save the current error
					errors = append(errors, err)
				} else if !discardNoSupportErrors {
					// Save the ErrNoSupport error, because it's either the first error or all encountered errors
					// were also ErrNoSupport errors.
					errors = append(errors, err)
				}
				continue
			}
			errors = append(errors, err)
			logrus.Debugf("Not continuing with error: %v", combineErrors(errors...).Error())
			return nil, combineErrors(errors...)
		}

		return imgInspect, nil
	}

	if len(errors) > 0 {
		return nil, combineErrors(errors...)
	}

	return nil, fmt.Errorf("no endpoints found for %s", ref.String())
}
