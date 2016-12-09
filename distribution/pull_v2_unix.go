// +build !windows

package distribution

import (
	"fmt"
	"path/filepath"

	"github.com/containers/image/docker"
	containersImageRef "github.com/containers/image/docker/reference"
	"github.com/containers/image/manifest"
	"github.com/containers/image/signature"
	"github.com/containers/image/types"
	"github.com/docker/distribution"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/dockerversion"
	"github.com/docker/docker/registry"
	"github.com/opencontainers/go-digest"
	gctx "golang.org/x/net/context"
)

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

func (p *v2Puller) checkTrusted(c gctx.Context, ref reference.Named) (reference.Named, error) {
	p.originalRef = ref
	// we can't use upstream docker/docker/reference since in projectatomic/docker
	// we modified docker/docker/reference and it's not doing any normalization.
	// we instead forked docker/docker/reference in containers/image and we need
	// this parsing here to make sure signature naming checks are consistent.
	dockerRef, err := containersImageRef.ParseNormalizedNamed(ref.String())
	if err != nil {
		return nil, err
	}
	imgRef, err := docker.NewReference(dockerRef)
	if err != nil {
		return nil, err
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
	img, err := imgRef.NewImage(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := p.policyContext.IsRunningImageAllowed(img)
	if !allowed {
		if err != nil {
			return nil, fmt.Errorf("%s isn't allowed: %v", ref.String(), err)
		}
		return nil, fmt.Errorf("%s isn't allowed", ref.String())
	}
	if err != nil {
		return nil, err
	}
	mfst, _, err := img.Manifest()
	if err != nil {
		return nil, err
	}
	dgst, err := manifest.Digest(mfst)
	if err != nil {
		return nil, err
	}
	ref, err = reference.WithDigest(reference.TrimNamed(ref), digest.Digest(dgst))
	if err != nil {
		return nil, err
	}
	return ref, nil
}
