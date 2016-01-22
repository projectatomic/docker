package image

import (
	"golang.org/x/net/context"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/cli"
	"github.com/docker/docker/cli/command"
	"github.com/docker/docker/pkg/jsonmessage"
	refutils "github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/spf13/cobra"
)

// NewPushCommand creates a new `docker push` command
func NewPushCommand(dockerCli *command.DockerCli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [OPTIONS] NAME[:TAG]",
		Short: "Push an image or a repository to a registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(dockerCli, args[0])
		},
	}

	flags := cmd.Flags()

	command.AddTrustSigningFlags(flags)

	return cmd
}

func runPush(dockerCli *command.DockerCli, remote string) error {
	ref, err := reference.ParseNormalizedNamed(remote)
	if err != nil {
		return err
	}

	// Resolve the Repository name from fqn to RepositoryInfo
	repoInfo, err := registry.ParseRepositoryInfo(ref)
	if err != nil {
		return err
	}

	ctx := context.Background()

	requestPrivilege := command.RegistryAuthenticationPrivilegedFunc(dockerCli, repoInfo.Index, "push", refutils.IsReferenceFullyQualified(ref))

	if command.IsTrusted() {
		return trustedPush(ctx, dockerCli, repoInfo, ref, requestPrivilege)
	}

	responseBody, err := imagePushPrivileged(ctx, dockerCli, ref, requestPrivilege)
	if err != nil {
		return err
	}

	defer responseBody.Close()
	return jsonmessage.DisplayJSONMessagesToStream(responseBody, dockerCli.Out(), nil)
}
