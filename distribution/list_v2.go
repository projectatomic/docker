package distribution

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/api/errcode"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

type v2TagLister struct {
	endpoint registry.APIEndpoint
	config   *ListRemoteTagsConfig
	repoInfo *registry.RepositoryInfo
	repo     distribution.Repository
	// confirmedV2 is set to true if we confirm we're talking to a v2
	// registry. This is used to limit fallbacks to the v1 protocol.
	confirmedV2 bool
}

func (tl *v2TagLister) ListTags(ctx context.Context) (tagList []*types.RepositoryTag, err error) {
	tl.repo, tl.confirmedV2, err = NewV2Repository(ctx, tl.repoInfo, tl.endpoint, tl.config.MetaHeaders, tl.config.AuthConfig, "pull")
	if err != nil {
		logrus.Debugf("Error getting v2 registry: %v", err)
		return nil, fallbackError{err: err, confirmedV2: tl.confirmedV2}
	}

	tagList, err = tl.listTagsWithRepository(ctx)
	if err != nil {
		switch t := err.(type) {
		case errcode.Errors:
			if len(t) == 1 {
				err = t[0]
			}
		}
		if registry.ContinueOnError(err) {
			logrus.Debugf("Error trying v2 registry: %v", err)
			err = fallbackError{err: err, confirmedV2: tl.confirmedV2}
		}
	}
	return
}

func (tl *v2TagLister) listTagsWithRepository(ctx context.Context) ([]*types.RepositoryTag, error) {
	logrus.Debugf("Retrieving the tag list from V2 endpoint %v", tl.endpoint.URL)
	tags, err := tl.repo.Tags(ctx).All(ctx)
	if err != nil {
		return nil, err
	}
	tagList := make([]*types.RepositoryTag, len(tags))
	for i, tag := range tags {
		tagList[i] = &types.RepositoryTag{Tag: tag}
	}
	return tagList, nil
}
