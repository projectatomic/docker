package distribution

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/api/errcode"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/registry"
	"golang.org/x/net/context"
)

type v2TagLister struct {
	endpoint registry.APIEndpoint
	config   *ListRemoteTagsConfig
	repoInfo *registry.RepositoryInfo
	repo     distribution.Repository
}

func (tl *v2TagLister) ListTags() (tagList []*types.RepositoryTag, fallback bool, err error) {
	tl.repo, err = NewV2Repository(tl.repoInfo, tl.endpoint, tl.config.MetaHeaders, tl.config.AuthConfig)
	if err != nil {
		logrus.Debugf("Error getting v2 registry: %v", err)
		return nil, true, err
	}

	tagList, err = tl.listTagsWithRepository()
	if err != nil && registry.ContinueOnError(err) {
		logrus.Debugf("Error trying v2 registry: %v", err)
		fallback = true
	}
	return
}

func (tl *v2TagLister) listTagsWithRepository() ([]*types.RepositoryTag, error) {
	logrus.Debugf("Retrieving the tag list from V2 endpoint %v", tl.endpoint.URL)
	manSvc, err := tl.repo.Manifests(context.Background())
	if err != nil {
		return nil, err
	}
	tags, err := manSvc.Tags()
	if err != nil {
		switch t := err.(type) {
		case errcode.Errors:
			if len(t) == 1 {
				return nil, t[0]
			}
		}
		return nil, err
	}
	tagList := make([]*types.RepositoryTag, len(tags))
	for i, tag := range tags {
		tagList[i] = &types.RepositoryTag{Tag: tag}
	}
	return tagList, nil
}
