package distribution

import (
	"fmt"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/registry/client/transport"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/registry"
)

type v1TagLister struct {
	endpoint registry.APIEndpoint
	config   *ListRemoteTagsConfig
	repoInfo *registry.RepositoryInfo
	session  *registry.Session
}

func (tl *v1TagLister) ListTags() ([]*types.RepositoryTag, bool, error) {
	tlsConfig, err := tl.config.RegistryService.TLSConfig(tl.repoInfo.Index.Name)
	if err != nil {
		return nil, false, err
	}
	// Adds Docker-specific headers as well as user-specified headers (metaHeaders)
	tr := transport.NewTransport(
		// TODO(tiborvass): was ReceiveTimeout
		registry.NewTransport(tlsConfig),
		registry.DockerHeaders(tl.config.MetaHeaders)...,
	)
	client := registry.HTTPClient(tr)
	v1Endpoint, err := tl.endpoint.ToV1Endpoint(tl.config.MetaHeaders)
	if err != nil {
		logrus.Debugf("Could not get v1 endpoint: %v", err)
		return nil, true, err
	}
	tl.session, err = registry.NewSession(client, tl.config.AuthConfig, v1Endpoint)
	if err != nil {
		// TODO(dmcgowan): Check if should fallback
		logrus.Debugf("Fallback from error: %s", err)
		return nil, true, err
	}
	tagList, err := tl.listTagsWithSession()
	return tagList, false, err
}

func (tl *v1TagLister) listTagsWithSession() ([]*types.RepositoryTag, error) {
	repoData, err := tl.session.GetRepositoryData(tl.repoInfo.RemoteName)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP code: 404") {
			return nil, fmt.Errorf("Error: image %s not found", tl.repoInfo.RemoteName)
		}
		// Unexpected HTTP error
		return nil, err
	}

	logrus.Debugf("Retrieving the tag list from V1 endpoints")
	tagsList, err := tl.session.GetRemoteTags(repoData.Endpoints, tl.repoInfo.RemoteName)
	if err != nil {
		logrus.Errorf("Unable to get remote tags: %s", err)
		return nil, err
	}
	if len(tagsList) < 1 {
		return nil, fmt.Errorf("No tags available for remote repository %s", tl.repoInfo.CanonicalName)
	}

	tagList := make([]*types.RepositoryTag, 0, len(tagsList))
	for tag, imageID := range tagsList {
		tagList = append(tagList, &types.RepositoryTag{Tag: tag, ImageID: imageID})
	}

	return tagList, nil
}
