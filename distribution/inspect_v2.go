package distribution

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/registry/api/errcode"
	"github.com/docker/distribution/registry/client"
	"github.com/docker/docker/image"
	"github.com/docker/docker/image/v1"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

type v2ManifestFetcher struct {
	endpoint registry.APIEndpoint
	config   *InspectConfig
	repoInfo *registry.RepositoryInfo
	repo     distribution.Repository
	// confirmedV2 is set to true if we confirm we're talking to a v2
	// registry. This is used to limit fallbacks to the v1 protocol.
	confirmedV2 bool
}

func (mf *v2ManifestFetcher) Fetch(ctx context.Context, ref reference.Named) (imgInspect *types.RemoteImageInspect, err error) {
	mf.repo, mf.confirmedV2, err = NewV2Repository(ctx, mf.repoInfo, mf.endpoint, mf.config.MetaHeaders, mf.config.AuthConfig, "pull")
	if err != nil {
		logrus.Debugf("Error getting v2 registry: %v", err)
		return nil, fallbackError{err: err, confirmedV2: mf.confirmedV2}
	}

	imgInspect, err = mf.fetchWithRepository(ctx, ref)
	if err != nil {
		switch t := err.(type) {
		case errcode.Errors:
			if len(t) == 1 {
				err = t[0]
			}
		}
		if registry.ContinueOnError(err) {
			logrus.Debugf("Error trying v2 registry: %v", err)
			err = fallbackError{err: err, confirmedV2: mf.confirmedV2}
		}
	}
	return
}

func (mf *v2ManifestFetcher) fetchWithRepository(ctx context.Context, ref reference.Named) (*types.RemoteImageInspect, error) {
	var (
		manifest    distribution.Manifest
		tagOrDigest string // Used for logging/progress only

		tag string
	)

	manSvc, err := mf.repo.Manifests(ctx)
	if err != nil {
		return nil, err
	}

	if digested, isDigested := ref.(reference.Canonical); isDigested {
		manifest, err = manSvc.Get(ctx, digested.Digest())
		if err != nil {
			return nil, err
		}
		tagOrDigest = digested.Digest().String()
	} else {
		if tagged, isTagged := ref.(reference.NamedTagged); isTagged {
			tagOrDigest = tagged.Tag()
			tag = tagOrDigest
		} else {
			tagList, err := mf.repo.Tags(ctx).All(ctx)
			if err != nil {
				return nil, err
			}
			for _, t := range tagList {
				if t == reference.DefaultTag {
					tag = t
				}
			}
			if tag == "" && len(tagList) > 0 {
				tag = tagList[0]
			}
			if tag == "" {
				return nil, fmt.Errorf("No tags available for remote repository %s", mf.repoInfo.FullName())
			}
		}
		// NOTE: not using TagService.Get, since it uses HEAD requests
		// against the manifests endpoint, which are not supported by
		// all registry versions.
		manifest, err = manSvc.Get(ctx, "", client.WithTag(tag))
		if err != nil {
			return nil, allowV1Fallback(err)
		}
	}

	if manifest == nil {
		return nil, fmt.Errorf("image manifest does not exist for tag or digest %q", tagOrDigest)
	}

	// If manSvc.Get succeeded, we can be confident that the registry on
	// the other side speaks the v2 protocol.
	mf.confirmedV2 = true

	var (
		image          *image.Image
		manifestDigest digest.Digest
	)

	switch v := manifest.(type) {
	case *schema1.SignedManifest:
		image, manifestDigest, err = mf.pullSchema1(ctx, ref, v)
		if err != nil {
			return nil, err
		}
	case *schema2.DeserializedManifest:
		image, manifestDigest, err = mf.pullSchema2(ctx, ref, v)
		if err != nil {
			return nil, err
		}
	case *manifestlist.DeserializedManifestList:
		image, manifestDigest, err = mf.pullManifestList(ctx, ref, v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported manifest format")
	}

	return makeRemoteImageInspect(mf.repoInfo, image, tag, manifestDigest), nil
}

func (mf *v2ManifestFetcher) pullSchema1(ctx context.Context, ref reference.Named, unverifiedManifest *schema1.SignedManifest) (img *image.Image, manifestDigest digest.Digest, err error) {
	var verifiedManifest *schema1.Manifest
	verifiedManifest, err = verifySchema1Manifest(unverifiedManifest, ref)
	if err != nil {
		return nil, "", err
	}

	// remove duplicate layers and check parent chain validity
	err = fixManifestLayers(verifiedManifest)
	if err != nil {
		return nil, "", err
	}

	// Image history converted to the new format
	var history []image.History

	// Note that the order of this loop is in the direction of bottom-most
	// to top-most, so that the downloads slice gets ordered correctly.
	for i := len(verifiedManifest.FSLayers) - 1; i >= 0; i-- {
		var throwAway struct {
			ThrowAway bool `json:"throwaway,omitempty"`
		}
		if err := json.Unmarshal([]byte(verifiedManifest.History[i].V1Compatibility), &throwAway); err != nil {
			return nil, "", err
		}

		h, err := v1.HistoryFromConfig([]byte(verifiedManifest.History[i].V1Compatibility), throwAway.ThrowAway)
		if err != nil {
			return nil, "", err
		}
		history = append(history, h)
	}

	rootFS := image.NewRootFS()
	configRaw, err := v1.MakeRawConfigFromV1Config([]byte(verifiedManifest.History[0].V1Compatibility), rootFS, history)
	if err != nil {
		return nil, "", err
	}

	config, err := json.Marshal(configRaw)
	if err != nil {
		return nil, "", err
	}

	img, err = image.NewFromJSON(config)
	if err != nil {
		return nil, "", err
	}

	manifestDigest = digest.FromBytes(unverifiedManifest.Canonical)

	return img, manifestDigest, nil
}

func (mf *v2ManifestFetcher) pullSchema2(ctx context.Context, ref reference.Named, mfst *schema2.DeserializedManifest) (img *image.Image, manifestDigest digest.Digest, err error) {
	manifestDigest, err = schema2ManifestDigest(ref, mfst)
	if err != nil {
		return nil, "", err
	}

	target := mfst.Target()

	configChan := make(chan []byte, 1)
	errChan := make(chan error, 1)
	var cancel func()
	ctx, cancel = context.WithCancel(ctx)

	// Pull the image config
	go func() {
		configJSON, err := mf.pullSchema2ImageConfig(ctx, target.Digest)
		if err != nil {
			errChan <- err
			cancel()
			return
		}
		configChan <- configJSON
	}()

	var (
		configJSON         []byte      // raw serialized image config
		unmarshalledConfig image.Image // deserialized image config
	)
	if runtime.GOOS == "windows" {
		configJSON, unmarshalledConfig, err = receiveConfig(configChan, errChan)
		if err != nil {
			return nil, "", err
		}
		if unmarshalledConfig.RootFS == nil {
			return nil, "", errors.New("image config has no rootfs section")
		}
	}

	if configJSON == nil {
		configJSON, unmarshalledConfig, err = receiveConfig(configChan, errChan)
		if err != nil {
			return nil, "", err
		}
	}

	img, err = image.NewFromJSON(configJSON)
	if err != nil {
		return nil, "", err
	}

	return img, manifestDigest, nil
}

func (mf *v2ManifestFetcher) pullSchema2ImageConfig(ctx context.Context, dgst digest.Digest) (configJSON []byte, err error) {
	blobs := mf.repo.Blobs(ctx)
	configJSON, err = blobs.Get(ctx, dgst)
	if err != nil {
		return nil, err
	}

	// Verify image config digest
	verifier, err := digest.NewDigestVerifier(dgst)
	if err != nil {
		return nil, err
	}
	if _, err := verifier.Write(configJSON); err != nil {
		return nil, err
	}
	if !verifier.Verified() {
		err := fmt.Errorf("image config verification failed for digest %s", dgst)
		logrus.Error(err)
		return nil, err
	}

	return configJSON, nil
}

// pullManifestList handles "manifest lists" which point to various
// platform-specifc manifests.
func (mf *v2ManifestFetcher) pullManifestList(ctx context.Context, ref reference.Named, mfstList *manifestlist.DeserializedManifestList) (img *image.Image, manifestListDigest digest.Digest, err error) {
	manifestListDigest, err = schema2ManifestDigest(ref, mfstList)
	if err != nil {
		return nil, "", err
	}

	var manifestDigest digest.Digest
	for _, manifestDescriptor := range mfstList.Manifests {
		// TODO(aaronl): The manifest list spec supports optional
		// "features" and "variant" fields. These are not yet used.
		// Once they are, their values should be interpreted here.
		if manifestDescriptor.Platform.Architecture == runtime.GOARCH && manifestDescriptor.Platform.OS == runtime.GOOS {
			manifestDigest = manifestDescriptor.Digest
			break
		}
	}

	if manifestDigest == "" {
		return nil, "", errors.New("no supported platform found in manifest list")
	}

	manSvc, err := mf.repo.Manifests(ctx)
	if err != nil {
		return nil, "", err
	}

	manifest, err := manSvc.Get(ctx, manifestDigest)
	if err != nil {
		return nil, "", err
	}

	manifestRef, err := reference.WithDigest(ref, manifestDigest)
	if err != nil {
		return nil, "", err
	}

	switch v := manifest.(type) {
	case *schema1.SignedManifest:
		img, _, err = mf.pullSchema1(ctx, manifestRef, v)
		if err != nil {
			return nil, "", err
		}
	case *schema2.DeserializedManifest:
		img, _, err = mf.pullSchema2(ctx, manifestRef, v)
		if err != nil {
			return nil, "", err
		}
	default:
		return nil, "", errors.New("unsupported manifest format")
	}

	return img, manifestListDigest, err
}
