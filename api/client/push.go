package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	Cli "github.com/docker/docker/cli"
	"github.com/docker/docker/pkg/jsonmessage"
	flag "github.com/docker/docker/pkg/mflag"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
)

func (cli *DockerCli) confirmPush() bool {
	const prompt = "Do you really want to push to public registry? [y/n]: "
	answer := ""
	fmt.Fprintln(cli.out, "")

	for answer != "n" && answer != "y" {
		fmt.Fprint(cli.out, prompt)
		answer = strings.ToLower(strings.TrimSpace(readInput(cli.in, cli.out)))
	}

	if answer == "n" {
		fmt.Fprintln(cli.out, "Nothing pushed.")
	}

	return answer == "y"
}

// CmdPush pushes an image or repository to the registry.
//
// Usage: docker push NAME[:TAG]
func (cli *DockerCli) CmdPush(args ...string) error {
	cmd := Cli.Subcmd("push", []string{"NAME[:TAG]"}, Cli.DockerCommands["push"].Description, true)
	force := cmd.Bool([]string{"f", "-force"}, false, "Push to public registry without confirmation")
	addTrustedFlags(cmd, false)
	cmd.Require(flag.Exact, 1)

	cmd.ParseFlags(args, true)

	ref, err := reference.ParseNamed(cmd.Arg(0))
	if err != nil {
		return err
	}

	var tag string
	switch x := ref.(type) {
	case reference.Canonical:
		return errors.New("cannot push a digest reference")
	case reference.NamedTagged:
		tag = x.Tag()
	}

	// Resolve the Repository name from fqn to RepositoryInfo
	repoInfo, err := registry.ParseRepositoryInfo(ref)
	if err != nil {
		return err
	}
	// Resolve the Auth config relevant for this server
	authConfig := registry.ResolveAuthConfig(cli.configFile.AuthConfigs, repoInfo.Index)

	requestPrivilege := cli.registryAuthenticationPrivilegedFunc(repoInfo.Index, "push")
	if isTrusted() {
		return cli.trustedPush(repoInfo, tag, *force, authConfig, requestPrivilege)
	}

	responseBody, err := cli.imagePushPrivileged(authConfig, ref.Name(), tag, *force, requestPrivilege)
	if err != nil || responseBody == nil {
		return err
	}

	defer responseBody.Close()

	return jsonmessage.DisplayJSONMessagesStream(responseBody, cli.out, cli.outFd, cli.isTerminalOut, nil)
}

func (cli *DockerCli) imagePushPrivileged(authConfig types.AuthConfig, imageID, tag string, force bool, requestPrivilege client.RequestPrivilegeFunc) (io.ReadCloser, error) {
	encodedAuth, err := encodeAuthToBase64(authConfig)
	if err != nil {
		return nil, err
	}
	options := types.ImagePushOptions{
		ImagePullOptions: types.ImagePullOptions{
			ImageID:      imageID,
			Tag:          tag,
			RegistryAuth: encodedAuth,
		},
		Force: force,
	}

	push := func() (io.ReadCloser, error) {
		return cli.client.ImagePush(options, requestPrivilege)
	}

	responseBody, err := push()
	if err != nil {
		if strings.Contains(err.Error(), fmt.Sprintf("Status %d", http.StatusForbidden)) && !force {
			if !cli.confirmPush() {
				return nil, nil
			}
			options.Force = true
			responseBody, err = push()
		}
	}

	return responseBody, err
}
