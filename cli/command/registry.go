package command

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/net/context"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	registrytypes "github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/pkg/term"
	refutils "github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
)

// GetEncodedAuth TODO
func GetEncodedAuth(cli *DockerCli, ref reference.Named) (string, error) {
	repoInfo, err := registry.ParseRepositoryInfo(ref)
	if err != nil {
		return "", err
	}
	auths := make(map[string]types.AuthConfig)
	if refutils.IsReferenceFullyQualified(ref) {
		authConfig := ResolveAuthConfig(context.Background(), cli, repoInfo.Index)
		authConfigKey := registry.GetAuthConfigKey(repoInfo.Index)
		auths[authConfigKey] = authConfig
	} else {
		auths, _ = cli.GetAllCredentials()
	}
	encoded, err := EncodeAuthToBase64(auths)
	if err != nil {
		return "", err
	}
	return encoded, nil
}

// ElectAuthServer returns the default registry to use (by asking the daemon)
func ElectAuthServer(ctx context.Context, cli *DockerCli) string {
	// The daemon `/info` endpoint informs us of the default registry being
	// used. This is essential in cross-platforms environment, where for
	// example a Linux client might be interacting with a Windows daemon, hence
	// the default registry URL might be Windows specific.
	serverAddress := registry.IndexServer
	if info, err := cli.Client().Info(ctx); err != nil {
		fmt.Fprintf(cli.Out(), "Warning: failed to get default registry endpoint from daemon (%v). Using system default: %s\n", err, serverAddress)
	} else {
		serverAddress = info.IndexServerAddress
	}
	return serverAddress
}

// EncodeAuthToBase64 serializes the auth configuration as JSON base64 payload
func EncodeAuthToBase64(authConfigs map[string]types.AuthConfig) (string, error) {
	buf, err := json.Marshal(authConfigs)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

// RegistryAuthenticationPrivilegedFunc returns a RequestPrivilegeFunc from the specified registry index info
// for the given command.
func RegistryAuthenticationPrivilegedFunc(cli *DockerCli, index *registrytypes.IndexInfo, cmdName string, singleAuth bool) types.RequestPrivilegeFunc {
	return func() (string, error) {
		fmt.Fprintf(cli.Out(), "\nPlease login prior to %s:\n", cmdName)
		indexServer := registry.GetAuthConfigKey(index)
		isDefaultRegistry := indexServer == ElectAuthServer(context.Background(), cli)
		authConfig, err := ConfigureAuth(cli, "", "", indexServer, isDefaultRegistry)
		if err != nil {
			return "", err
		}
		auths := make(map[string]types.AuthConfig)
		if singleAuth {
			auths[indexServer] = authConfig
		} else {
			auths = cli.configFile.AuthConfigs
		}
		return EncodeAuthToBase64(auths)
	}
}

// ResolveAuthConfig is like registry.ResolveAuthConfig, but if using the
// default index, it uses the default index name for the daemon's platform,
// not the client's platform.
func ResolveAuthConfig(ctx context.Context, cli *DockerCli, index *registrytypes.IndexInfo) types.AuthConfig {
	configKey := index.Name
	if index.Official {
		configKey = "https://index.docker.io/v1/"
	}

	a, _ := cli.CredentialsStore(configKey).Get(configKey)
	return a
}

// ConfigureAuth returns an AuthConfig from the specified user, password and server.
func ConfigureAuth(cli *DockerCli, flUser, flPassword, serverAddress string, isDefaultRegistry bool) (types.AuthConfig, error) {
	// On Windows, force the use of the regular OS stdin stream. Fixes #14336/#14210
	if runtime.GOOS == "windows" {
		cli.in = NewInStream(os.Stdin)
	}

	if !isDefaultRegistry {
		serverAddress = registry.ConvertToHostname(serverAddress)
	}

	authconfig, err := cli.CredentialsStore(serverAddress).Get(serverAddress)
	if err != nil {
		return authconfig, err
	}

	// Some links documenting this:
	// - https://code.google.com/archive/p/mintty/issues/56
	// - https://github.com/docker/docker/issues/15272
	// - https://mintty.github.io/ (compatibility)
	// Linux will hit this if you attempt `cat | docker login`, and Windows
	// will hit this if you attempt docker login from mintty where stdin
	// is a pipe, not a character based console.
	if flPassword == "" && !cli.In().IsTerminal() {
		return authconfig, fmt.Errorf("Error: Cannot perform an interactive login from a non TTY device")
	}

	authconfig.Username = strings.TrimSpace(authconfig.Username)

	if flUser = strings.TrimSpace(flUser); flUser == "" {
		if serverAddress == "https://index.docker.io/v1/" {
			// if this is a default registry (docker hub), then display the following message.
			fmt.Fprintln(cli.Out(), "Login with your Docker ID to push and pull images from Docker Hub. If you don't have a Docker ID, head over to https://hub.docker.com to create one.")
		}
		promptWithDefault(cli.Out(), "Username", authconfig.Username)
		flUser = readInput(cli.In(), cli.Out())
		flUser = strings.TrimSpace(flUser)
		if flUser == "" {
			flUser = authconfig.Username
		}
	}
	if flUser == "" {
		return authconfig, fmt.Errorf("Error: Non-null Username Required")
	}
	if flPassword == "" {
		oldState, err := term.SaveState(cli.In().FD())
		if err != nil {
			return authconfig, err
		}
		fmt.Fprintf(cli.Out(), "Password: ")
		term.DisableEcho(cli.In().FD(), oldState)

		flPassword = readInput(cli.In(), cli.Out())
		fmt.Fprint(cli.Out(), "\n")

		term.RestoreTerminal(cli.In().FD(), oldState)
		if flPassword == "" {
			return authconfig, fmt.Errorf("Error: Password Required")
		}
	}

	authconfig.Username = flUser
	authconfig.Password = flPassword
	authconfig.ServerAddress = serverAddress
	authconfig.IdentityToken = ""

	return authconfig, nil
}

func readInput(in io.Reader, out io.Writer) string {
	reader := bufio.NewReader(in)
	line, _, err := reader.ReadLine()
	if err != nil {
		fmt.Fprintln(out, err.Error())
		os.Exit(1)
	}
	return string(line)
}

func promptWithDefault(out io.Writer, prompt string, configDefault string) {
	if configDefault == "" {
		fmt.Fprintf(out, "%s: ", prompt)
	} else {
		fmt.Fprintf(out, "%s (%s): ", prompt, configDefault)
	}
}

// RetrieveAuthTokenFromImage retrieves an encoded auth token given a complete image
func RetrieveAuthTokenFromImage(ctx context.Context, cli *DockerCli, image string) (string, error) {
	// Retrieve encoded auth token from the image reference
	auths, err := resolveAuthConfigFromImage(ctx, cli, image)
	if err != nil {
		return "", err
	}
	encodedAuth, err := EncodeAuthToBase64(auths)
	if err != nil {
		return "", err
	}
	return encodedAuth, nil
}

// resolveAuthConfigFromImage retrieves that AuthConfig using the image string
func resolveAuthConfigFromImage(ctx context.Context, cli *DockerCli, image string) (map[string]types.AuthConfig, error) {
	auths := make(map[string]types.AuthConfig)
	registryRef, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return auths, err
	}
	repoInfo, err := registry.ParseRepositoryInfo(registryRef)
	if err != nil {
		return auths, err
	}
	authConfig := ResolveAuthConfig(ctx, cli, repoInfo.Index)
	authConfigKey := registry.GetAuthConfigKey(repoInfo.Index)
	auths[authConfigKey] = authConfig
	return auths, nil
}
