package distribution

import (
	"fmt"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/registry/client/transport"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

type v1TagLister struct {
	endpoint registry.APIEndpoint
	config   *ListRemoteTagsConfig
	repoInfo *registry.RepositoryInfo
	session  *registry.Session
}

func (tl *v1TagLister) ListTags(ctx context.Context) ([]*types.RepositoryTag, error) {
	tlsConfig, err := tl.config.RegistryService.TLSConfig(tl.repoInfo.Index.Name)
	if err != nil {
		return nil, err
	}
	// Adds Docker-specific headers as well as user-specified headers (metaHeaders)
	tr := transport.NewTransport(
		registry.NewTransport(tlsConfig),
		registry.DockerHeaders(tl.config.MetaHeaders)...,
	)
	client := registry.HTTPClient(tr)
	v1Endpoint, err := tl.endpoint.ToV1Endpoint(tl.config.MetaHeaders)
	if err != nil {
		logrus.Debugf("Could not get v1 endpoint: %v", err)
		return nil, fallbackError{err: err}
	}
	info, err := v1Endpoint.Ping()
	if err != nil {
		return nil, fallbackError{err: err}
	}
	logrus.Debugf("Got endpoint info for %q: version=%s, standalone=%t", v1Endpoint.String(), info.Version, info.Standalone)
	tl.session, err = registry.NewSession(client, tl.config.AuthConfig, v1Endpoint)
	if err != nil {
		return nil, fallbackError{err: err}
	}
	tagList, err := tl.listTagsWithSession(ctx)
	return tagList, err
}

func (tl *v1TagLister) listTagsWithSession(ctx context.Context) ([]*types.RepositoryTag, error) {
	repoData, err := tl.session.GetRepositoryData(tl.repoInfo)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP code: 404") {
			// try with v2
			return nil, fallbackError{err: fmt.Errorf("Error: image %s not found", tl.repoInfo.RemoteName())}
		}
		// Unexpected HTTP error
		return nil, err
	}

	logrus.Debugf("Retrieving the tag list from V1 endpoints")
	tagsList, err := tl.session.GetRemoteTags(repoData.Endpoints, tl.repoInfo)
	if err != nil {
		logrus.Errorf("Unable to get remote tags: %v", err)
		return nil, err
	}
	if len(tagsList) < 1 {
		return nil, fmt.Errorf("No tags available for remote repository %s", tl.repoInfo.FullName())
	}

	tagList := make([]*types.RepositoryTag, 0, len(tagsList))
	for tag, imageID := range tagsList {
		tagList = append(tagList, &types.RepositoryTag{Tag: tag, ImageID: imageID})
	}

	return tagList, nil
}
