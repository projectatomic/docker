// +build !windows

package distribution

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/containers/image/docker"
	"github.com/containers/image/docker/daemon/signatures"
	containersImageRef "github.com/containers/image/docker/reference"
	ciImage "github.com/containers/image/image"
	"github.com/containers/image/manifest"
	"github.com/containers/image/signature"
	"github.com/containers/image/types"
	"github.com/docker/distribution"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/dockerversion"
	"github.com/docker/docker/image"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	gctx "golang.org/x/net/context"
)

func detectBaseLayer(is image.Store, m *schema1.Manifest, rootFS *image.RootFS) error {
	return nil
}

func (ld *v2LayerDescriptor) open(ctx context.Context) (distribution.ReadSeekCloser, error) {
	blobs := ld.repo.Blobs(ctx)
	return blobs.Open(ctx, ld.digest)
}

func configurePolicyContext() (*signature.PolicyContext, error) {
	defaultPolicy, err := signature.DefaultPolicy(nil)
	if err != nil {
		return nil, err
	}
	pc, err := signature.NewPolicyContext(defaultPolicy)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ciImage returns a *containers/image/image.UnparsedImage and a close callback for ref.
func (p *v2Puller) ciImage(c gctx.Context, ref reference.Named) (*ciImage.UnparsedImage, io.Closer, error) {
	// we can't use upstream docker/docker/reference since in projectatomic/docker
	// we modified docker/docker/reference and it's not doing any normalization.
	// we instead forked docker/docker/reference in containers/image and we need
	// this parsing here to make sure signature naming checks are consistent.
	dockerRef, err := containersImageRef.ParseNormalizedNamed(ref.String())
	if err != nil {
		return nil, nil, err
	}
	imgRef, err := docker.NewReference(dockerRef)
	if err != nil {
		return nil, nil, err
	}
	isSecure := (p.endpoint.TLSConfig == nil || !p.endpoint.TLSConfig.InsecureSkipVerify)
	authConfig := registry.ResolveAuthConfig(p.config.AuthConfigs, p.repoInfo.Index)
	dockerAuthConfig := types.DockerAuthConfig{
		Username: authConfig.Username,
		Password: authConfig.Password,
	}
	ctx := &types.SystemContext{
		DockerDisableV1Ping:         p.config.V2Only,
		DockerInsecureSkipTLSVerify: !isSecure,
		DockerAuthConfig:            &dockerAuthConfig,
		DockerRegistryUserAgent:     dockerversion.DockerUserAgent(c),
	}
	if p.config.RegistryService.SecureIndex(p.repoInfo.Index.Name) {
		ctx.DockerCertPath = filepath.Join(registry.CertsDir, p.repoInfo.Index.Name)
	}
	src, err := imgRef.NewImageSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	unparsed := ciImage.UnparsedInstance(src, nil)
	return unparsed, src, nil
}

func (p *v2Puller) checkTrusted(ref reference.Named, unparsed types.UnparsedImage) (reference.Named, error) {
	p.originalRef = ref
	allowed, err := p.policyContext.IsRunningImageAllowed(unparsed)
	if !allowed {
		if err != nil {
			return nil, fmt.Errorf("%s isn't allowed: %v", ref.String(), err)
		}
		return nil, fmt.Errorf("%s isn't allowed", ref.String())
	}
	if err != nil {
		return nil, err
	}
	mfst, _, err := unparsed.Manifest()
	if err != nil {
		return nil, err
	}
	dgst, err := manifest.Digest(mfst)
	if err != nil {
		return nil, err
	}
	ref, err = reference.WithDigest(ref, digest.Digest(dgst))
	if err != nil {
		return nil, err
	}
	return ref, nil
}

// storeSignature stores the signatures of ciImage and updates the tag in ciImage.Reference() if necessary.
func (p *v2Puller) storeSignatures(c gctx.Context, unparsed *ciImage.UnparsedImage) error {
	img, err := ciImage.FromUnparsedImage(nil, unparsed)
	if err != nil {
		return err
	}
	store := signatures.NewStore(nil)
	return store.RecordImage(c, img)
}
