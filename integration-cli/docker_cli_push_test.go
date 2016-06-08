package main

import (
	"archive/tar"
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/cliconfig"
	"github.com/docker/docker/pkg/integration/checker"
	"github.com/go-check/check"
)

// Pushing an image to a private registry.
func testPushBusyboxImage(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	// tag the image to upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)
	// push the image to the registry
	dockerCmd(c, "push", repoName)
}

func (s *DockerRegistrySuite) TestPushBusyboxImage(c *check.C) {
	testPushBusyboxImage(c)
}

func (s *DockerSchema1RegistrySuite) TestPushBusyboxImage(c *check.C) {
	testPushBusyboxImage(c)
}

// pushing an image without a prefix should throw an error
func (s *DockerSuite) TestPushUnprefixedRepo(c *check.C) {
	out, _, err := dockerCmdWithError("push", "busybox")
	c.Assert(err, check.NotNil, check.Commentf("pushing an unprefixed repo didn't result in a non-zero exit status: %s", out))
}

func testPushUntagged(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	expected := "An image does not exist locally with the tag"

	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf("pushing the image to the private registry should have failed: output %q", out))
	c.Assert(out, checker.Contains, expected, check.Commentf("pushing the image failed"))
}

func (s *DockerRegistrySuite) TestPushUntagged(c *check.C) {
	testPushUntagged(c)
}

func (s *DockerSchema1RegistrySuite) TestPushUntagged(c *check.C) {
	testPushUntagged(c)
}

func testPushBadTag(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox:latest", privateRegistryURL)
	expected := "does not exist"

	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf("pushing the image to the private registry should have failed: output %q", out))
	c.Assert(out, checker.Contains, expected, check.Commentf("pushing the image failed"))
}

func (s *DockerRegistrySuite) TestPushBadTag(c *check.C) {
	testPushBadTag(c)
}

func (s *DockerSchema1RegistrySuite) TestPushBadTag(c *check.C) {
	testPushBadTag(c)
}

func testPushMultipleTags(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	repoTag1 := fmt.Sprintf("%v/dockercli/busybox:t1", privateRegistryURL)
	repoTag2 := fmt.Sprintf("%v/dockercli/busybox:t2", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoTag1)

	dockerCmd(c, "tag", "busybox", repoTag2)

	dockerCmd(c, "push", repoName)

	// Ensure layer list is equivalent for repoTag1 and repoTag2
	out1, _ := dockerCmd(c, "pull", repoTag1)

	imageAlreadyExists := ": Image already exists"
	var out1Lines []string
	for _, outputLine := range strings.Split(out1, "\n") {
		if strings.Contains(outputLine, imageAlreadyExists) {
			out1Lines = append(out1Lines, outputLine)
		}
	}

	out2, _ := dockerCmd(c, "pull", repoTag2)

	var out2Lines []string
	for _, outputLine := range strings.Split(out2, "\n") {
		if strings.Contains(outputLine, imageAlreadyExists) {
			out1Lines = append(out1Lines, outputLine)
		}
	}
	c.Assert(out2Lines, checker.HasLen, len(out1Lines))

	for i := range out1Lines {
		c.Assert(out1Lines[i], checker.Equals, out2Lines[i])
	}
}

func (s *DockerRegistrySuite) TestPushMultipleTags(c *check.C) {
	testPushMultipleTags(c)
}

func (s *DockerSchema1RegistrySuite) TestPushMultipleTags(c *check.C) {
	testPushMultipleTags(c)
}

func testPushEmptyLayer(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/emptylayer", privateRegistryURL)
	emptyTarball, err := ioutil.TempFile("", "empty_tarball")
	c.Assert(err, check.IsNil, check.Commentf("Unable to create test file"))

	tw := tar.NewWriter(emptyTarball)
	err = tw.Close()
	c.Assert(err, check.IsNil, check.Commentf("Error creating empty tarball"))

	freader, err := os.Open(emptyTarball.Name())
	c.Assert(err, check.IsNil, check.Commentf("Could not open test tarball"))

	importCmd := exec.Command(dockerBinary, "import", "-", repoName)
	importCmd.Stdin = freader
	out, _, err := runCommandWithOutput(importCmd)
	c.Assert(err, check.IsNil, check.Commentf("import failed: %q", out))

	// Now verify we can push it
	out, _, err = dockerCmdWithError("push", repoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out))
}

func (s *DockerRegistrySuite) TestPushEmptyLayer(c *check.C) {
	testPushEmptyLayer(c)
}

func (s *DockerSchema1RegistrySuite) TestPushEmptyLayer(c *check.C) {
	testPushEmptyLayer(c)
}

// testConcurrentPush pushes multiple tags to the same repo
// concurrently.
func testConcurrentPush(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)

	repos := []string{}
	for _, tag := range []string{"push1", "push2", "push3"} {
		repo := fmt.Sprintf("%v:%v", repoName, tag)
		_, err := buildImage(repo, fmt.Sprintf(`
	FROM busybox
	ENTRYPOINT ["/bin/echo"]
	ENV FOO foo
	ENV BAR bar
	CMD echo %s
`, repo), true)
		c.Assert(err, checker.IsNil)
		repos = append(repos, repo)
	}

	// Push tags, in parallel
	results := make(chan error)

	for _, repo := range repos {
		go func(repo string) {
			_, _, err := runCommandWithOutput(exec.Command(dockerBinary, "push", repo))
			results <- err
		}(repo)
	}

	for range repos {
		err := <-results
		c.Assert(err, checker.IsNil, check.Commentf("concurrent push failed with error: %v", err))
	}

	// Clear local images store.
	args := append([]string{"rmi"}, repos...)
	dockerCmd(c, args...)

	// Re-pull and run individual tags, to make sure pushes succeeded
	for _, repo := range repos {
		dockerCmd(c, "pull", repo)
		dockerCmd(c, "inspect", repo)
		out, _ := dockerCmd(c, "run", "--rm", repo)
		c.Assert(strings.TrimSpace(out), checker.Equals, "/bin/sh -c echo "+repo)
	}
}

func (s *DockerRegistrySuite) TestConcurrentPush(c *check.C) {
	testConcurrentPush(c)
}

func (s *DockerSchema1RegistrySuite) TestConcurrentPush(c *check.C) {
	testConcurrentPush(c)
}

func (s *DockerRegistrySuite) TestCrossRepositoryLayerPush(c *check.C) {
	sourceRepoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	// tag the image to upload it to the private registry
	dockerCmd(c, "tag", "busybox", sourceRepoName)
	// push the image to the registry
	out1, _, err := dockerCmdWithError("push", sourceRepoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out1))
	// ensure that none of the layers were mounted from another repository during push
	c.Assert(strings.Contains(out1, "Mounted from"), check.Equals, false)

	digest1 := reference.DigestRegexp.FindString(out1)
	c.Assert(len(digest1), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))

	destRepoName := fmt.Sprintf("%v/dockercli/crossrepopush", privateRegistryURL)
	// retag the image to upload the same layers to another repo in the same registry
	dockerCmd(c, "tag", "busybox", destRepoName)
	// push the image to the registry
	out2, _, err := dockerCmdWithError("push", destRepoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out2))
	// ensure that layers were mounted from the first repo during push
	c.Assert(strings.Contains(out2, "Mounted from dockercli/busybox"), check.Equals, true)

	digest2 := reference.DigestRegexp.FindString(out2)
	c.Assert(len(digest2), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))
	c.Assert(digest1, check.Equals, digest2)

	// ensure that pushing again produces the same digest
	out3, _, err := dockerCmdWithError("push", destRepoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out2))

	digest3 := reference.DigestRegexp.FindString(out3)
	c.Assert(len(digest2), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))
	c.Assert(digest3, check.Equals, digest2)

	// ensure that we can pull and run the cross-repo-pushed repository
	dockerCmd(c, "rmi", destRepoName)
	dockerCmd(c, "pull", destRepoName)
	out4, _ := dockerCmd(c, "run", destRepoName, "echo", "-n", "hello world")
	c.Assert(out4, check.Equals, "hello world")
}

func (s *DockerSchema1RegistrySuite) TestCrossRepositoryLayerPushNotSupported(c *check.C) {
	sourceRepoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	// tag the image to upload it to the private registry
	dockerCmd(c, "tag", "busybox", sourceRepoName)
	// push the image to the registry
	out1, _, err := dockerCmdWithError("push", sourceRepoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out1))
	// ensure that none of the layers were mounted from another repository during push
	c.Assert(strings.Contains(out1, "Mounted from"), check.Equals, false)

	digest1 := reference.DigestRegexp.FindString(out1)
	c.Assert(len(digest1), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))

	destRepoName := fmt.Sprintf("%v/dockercli/crossrepopush", privateRegistryURL)
	// retag the image to upload the same layers to another repo in the same registry
	dockerCmd(c, "tag", "busybox", destRepoName)
	// push the image to the registry
	out2, _, err := dockerCmdWithError("push", destRepoName)
	c.Assert(err, check.IsNil, check.Commentf("pushing the image to the private registry has failed: %s", out2))
	// schema1 registry should not support cross-repo layer mounts, so ensure that this does not happen
	c.Assert(strings.Contains(out2, "Mounted from"), check.Equals, false)

	digest2 := reference.DigestRegexp.FindString(out2)
	c.Assert(len(digest2), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))
	c.Assert(digest1, check.Not(check.Equals), digest2)

	// ensure that we can pull and run the second pushed repository
	dockerCmd(c, "rmi", destRepoName)
	dockerCmd(c, "pull", destRepoName)
	out3, _ := dockerCmd(c, "run", destRepoName, "echo", "-n", "hello world")
	c.Assert(out3, check.Equals, "hello world")
}

func (s *DockerTrustSuite) TestTrustedPush(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclitrusted/pushtest:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("Error running trusted push: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push"))

	// Try pull after push
	pullCmd := exec.Command(dockerBinary, "pull", repoName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.IsNil, check.Commentf(out))
	c.Assert(string(out), checker.Contains, "Status: Image is up to date", check.Commentf(out))

	// Assert that we rotated the snapshot key to the server by checking our local keystore
	contents, err := ioutil.ReadDir(filepath.Join(cliconfig.ConfigDir(), "trust/private/tuf_keys", privateRegistryURL, "dockerclitrusted/pushtest"))
	c.Assert(err, check.IsNil, check.Commentf("Unable to read local tuf key files"))
	// Check that we only have 1 key (targets key)
	c.Assert(contents, checker.HasLen, 1)
}

func (s *DockerTrustSuite) TestTrustedPushWithEnvPasswords(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclienv/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmdWithPassphrases(pushCmd, "12345678", "12345678")
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("Error running trusted push: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push"))

	// Try pull after push
	pullCmd := exec.Command(dockerBinary, "pull", repoName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.IsNil, check.Commentf(out))
	c.Assert(string(out), checker.Contains, "Status: Image is up to date", check.Commentf(out))
}

func (s *DockerTrustSuite) TestTrustedPushWithFailingServer(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclitrusted/failingserver:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	pushCmd := exec.Command(dockerBinary, "push", repoName)
	// Using a name that doesn't resolve to an address makes this test faster
	s.trustedCmdWithServer(pushCmd, "https://server.invalid:81/")
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.NotNil, check.Commentf("Missing error while running trusted push w/ no server"))
	c.Assert(out, checker.Contains, "error contacting notary server", check.Commentf("Missing expected output on trusted push"))
}

func (s *DockerTrustSuite) TestTrustedPushWithoutServerAndUntrusted(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclitrusted/trustedandnot:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	pushCmd := exec.Command(dockerBinary, "push", "--disable-content-trust", repoName)
	// Using a name that doesn't resolve to an address makes this test faster
	s.trustedCmdWithServer(pushCmd, "https://server.invalid")
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push with no server and --disable-content-trust failed: %s\n%s", err, out))
	c.Assert(out, check.Not(checker.Contains), "Error establishing connection to notary repository", check.Commentf("Missing expected output on trusted push with --disable-content-trust:"))
}

func (s *DockerTrustSuite) TestTrustedPushWithExistingTag(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclitag/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)
	dockerCmd(c, "push", repoName)

	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))

	// Try pull after push
	pullCmd := exec.Command(dockerBinary, "pull", repoName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.IsNil, check.Commentf(out))
	c.Assert(string(out), checker.Contains, "Status: Image is up to date", check.Commentf(out))
}

func (s *DockerTrustSuite) TestTrustedPushWithExistingSignedTag(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclipushpush/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	// Do a trusted push
	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))

	// Do another trusted push
	pushCmd = exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err = runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))

	dockerCmd(c, "rmi", repoName)

	// Try pull to ensure the double push did not break our ability to pull
	pullCmd := exec.Command(dockerBinary, "pull", repoName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.IsNil, check.Commentf("Error running trusted pull: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Status: Downloaded", check.Commentf("Missing expected output on trusted pull with --disable-content-trust"))

}

func (s *DockerTrustSuite) TestTrustedPushWithIncorrectPassphraseForNonRoot(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercliincorretpwd/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	// Push with default passphrases
	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push:\n%s", out))

	// Push with wrong passphrases
	pushCmd = exec.Command(dockerBinary, "push", repoName)
	s.trustedCmdWithPassphrases(pushCmd, "12345678", "87654321")
	out, _, err = runCommandWithOutput(pushCmd)
	c.Assert(err, check.NotNil, check.Commentf("Error missing from trusted push with short targets passphrase: \n%s", out))
	c.Assert(out, checker.Contains, "could not find necessary signing keys", check.Commentf("Missing expected output on trusted push with short targets/snapsnot passphrase"))
}

func (s *DockerTrustSuite) TestTrustedPushWithExpiredSnapshot(c *check.C) {
	c.Skip("Currently changes system time, causing instability")
	repoName := fmt.Sprintf("%v/dockercliexpiredsnapshot/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	// Push with default passphrases
	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push"))

	// Snapshots last for three years. This should be expired
	fourYearsLater := time.Now().Add(time.Hour * 24 * 365 * 4)

	runAtDifferentDate(fourYearsLater, func() {
		// Push with wrong passphrases
		pushCmd = exec.Command(dockerBinary, "push", repoName)
		s.trustedCmd(pushCmd)
		out, _, err = runCommandWithOutput(pushCmd)
		c.Assert(err, check.NotNil, check.Commentf("Error missing from trusted push with expired snapshot: \n%s", out))
		c.Assert(out, checker.Contains, "repository out-of-date", check.Commentf("Missing expected output on trusted push with expired snapshot"))
	})
}

func (s *DockerTrustSuite) TestTrustedPushWithExpiredTimestamp(c *check.C) {
	c.Skip("Currently changes system time, causing instability")
	repoName := fmt.Sprintf("%v/dockercliexpiredtimestamppush/trusted:latest", privateRegistryURL)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)

	// Push with default passphrases
	pushCmd := exec.Command(dockerBinary, "push", repoName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push"))

	// The timestamps expire in two weeks. Lets check three
	threeWeeksLater := time.Now().Add(time.Hour * 24 * 21)

	// Should succeed because the server transparently re-signs one
	runAtDifferentDate(threeWeeksLater, func() {
		pushCmd := exec.Command(dockerBinary, "push", repoName)
		s.trustedCmd(pushCmd)
		out, _, err := runCommandWithOutput(pushCmd)
		c.Assert(err, check.IsNil, check.Commentf("Error running trusted push: %s\n%s", err, out))
		c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with expired timestamp"))
	})
}

func (s *DockerTrustSuite) TestTrustedPushWithReleasesDelegationOnly(c *check.C) {
	testRequires(c, NotaryHosting)
	repoName := fmt.Sprintf("%v/dockerclireleasedelegationinitfirst/trusted", privateRegistryURL)
	targetName := fmt.Sprintf("%s:latest", repoName)
	s.notaryInitRepo(c, repoName)
	s.notaryCreateDelegation(c, repoName, "targets/releases", s.not.keys[0].Public)
	s.notaryPublish(c, repoName)

	s.notaryImportKey(c, repoName, "targets/releases", s.not.keys[0].Private)

	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", targetName)

	pushCmd := exec.Command(dockerBinary, "push", targetName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))
	// check to make sure that the target has been added to targets/releases and not targets
	s.assertTargetInRoles(c, repoName, "latest", "targets/releases")
	s.assertTargetNotInRoles(c, repoName, "latest", "targets")

	// Try pull after push
	os.RemoveAll(filepath.Join(cliconfig.ConfigDir(), "trust"))

	pullCmd := exec.Command(dockerBinary, "pull", targetName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.IsNil, check.Commentf(out))
	c.Assert(string(out), checker.Contains, "Status: Image is up to date", check.Commentf(out))
}

func (s *DockerTrustSuite) TestTrustedPushSignsAllFirstLevelRolesWeHaveKeysFor(c *check.C) {
	testRequires(c, NotaryHosting)
	repoName := fmt.Sprintf("%v/dockerclimanyroles/trusted", privateRegistryURL)
	targetName := fmt.Sprintf("%s:latest", repoName)
	s.notaryInitRepo(c, repoName)
	s.notaryCreateDelegation(c, repoName, "targets/role1", s.not.keys[0].Public)
	s.notaryCreateDelegation(c, repoName, "targets/role2", s.not.keys[1].Public)
	s.notaryCreateDelegation(c, repoName, "targets/role3", s.not.keys[2].Public)

	// import everything except the third key
	s.notaryImportKey(c, repoName, "targets/role1", s.not.keys[0].Private)
	s.notaryImportKey(c, repoName, "targets/role2", s.not.keys[1].Private)

	s.notaryCreateDelegation(c, repoName, "targets/role1/subrole", s.not.keys[3].Public)
	s.notaryImportKey(c, repoName, "targets/role1/subrole", s.not.keys[3].Private)

	s.notaryPublish(c, repoName)

	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", targetName)

	pushCmd := exec.Command(dockerBinary, "push", targetName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))

	// check to make sure that the target has been added to targets/role1 and targets/role2, and
	// not targets (because there are delegations) or targets/role3 (due to missing key) or
	// targets/role1/subrole (due to it being a second level delegation)
	s.assertTargetInRoles(c, repoName, "latest", "targets/role1", "targets/role2")
	s.assertTargetNotInRoles(c, repoName, "latest", "targets")

	// Try pull after push
	os.RemoveAll(filepath.Join(cliconfig.ConfigDir(), "trust"))

	// pull should fail because none of these are the releases role
	pullCmd := exec.Command(dockerBinary, "pull", targetName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.NotNil, check.Commentf(out))
}

func (s *DockerTrustSuite) TestTrustedPushSignsForRolesWithKeysAndValidPaths(c *check.C) {
	repoName := fmt.Sprintf("%v/dockerclirolesbykeysandpaths/trusted", privateRegistryURL)
	targetName := fmt.Sprintf("%s:latest", repoName)
	s.notaryInitRepo(c, repoName)
	s.notaryCreateDelegation(c, repoName, "targets/role1", s.not.keys[0].Public, "l", "z")
	s.notaryCreateDelegation(c, repoName, "targets/role2", s.not.keys[1].Public, "x", "y")
	s.notaryCreateDelegation(c, repoName, "targets/role3", s.not.keys[2].Public, "latest")
	s.notaryCreateDelegation(c, repoName, "targets/role4", s.not.keys[3].Public, "latest")

	// import everything except the third key
	s.notaryImportKey(c, repoName, "targets/role1", s.not.keys[0].Private)
	s.notaryImportKey(c, repoName, "targets/role2", s.not.keys[1].Private)
	s.notaryImportKey(c, repoName, "targets/role4", s.not.keys[3].Private)

	s.notaryPublish(c, repoName)

	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", targetName)

	pushCmd := exec.Command(dockerBinary, "push", targetName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.IsNil, check.Commentf("trusted push failed: %s\n%s", err, out))
	c.Assert(out, checker.Contains, "Signing and pushing trust metadata", check.Commentf("Missing expected output on trusted push with existing tag"))

	// check to make sure that the target has been added to targets/role1 and targets/role4, and
	// not targets (because there are delegations) or targets/role2 (due to path restrictions) or
	// targets/role3 (due to missing key)
	s.assertTargetInRoles(c, repoName, "latest", "targets/role1", "targets/role4")
	s.assertTargetNotInRoles(c, repoName, "latest", "targets")

	// Try pull after push
	os.RemoveAll(filepath.Join(cliconfig.ConfigDir(), "trust"))

	// pull should fail because none of these are the releases role
	pullCmd := exec.Command(dockerBinary, "pull", targetName)
	s.trustedCmd(pullCmd)
	out, _, err = runCommandWithOutput(pullCmd)
	c.Assert(err, check.NotNil, check.Commentf(out))
}

func (s *DockerTrustSuite) TestTrustedPushDoesntSignTargetsIfDelegationsExist(c *check.C) {
	testRequires(c, NotaryHosting)
	repoName := fmt.Sprintf("%v/dockerclireleasedelegationnotsignable/trusted", privateRegistryURL)
	targetName := fmt.Sprintf("%s:latest", repoName)
	s.notaryInitRepo(c, repoName)
	s.notaryCreateDelegation(c, repoName, "targets/role1", s.not.keys[0].Public)
	s.notaryPublish(c, repoName)

	// do not import any delegations key

	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", targetName)

	pushCmd := exec.Command(dockerBinary, "push", targetName)
	s.trustedCmd(pushCmd)
	out, _, err := runCommandWithOutput(pushCmd)
	c.Assert(err, check.NotNil, check.Commentf("trusted push succeeded but should have failed:\n%s", out))
	c.Assert(out, checker.Contains, "no valid signing keys",
		check.Commentf("Missing expected output on trusted push without keys"))

	s.assertTargetNotInRoles(c, repoName, "latest", "targets", "targets/role1")
}

func (s *DockerRegistryAuthHtpasswdSuite) TestPushNoCredentialsNoRetry(c *check.C) {
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, check.Not(checker.Contains), "Retrying")
	c.Assert(out, checker.Contains, "no basic auth credentials")
}

// This may be flaky but it's needed not to regress on unauthorized push, see #21054
func (s *DockerSuite) TestPushToCentralRegistryUnauthorized(c *check.C) {
	testRequires(c, Network)
	repoName := "test/busybox"
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, check.Not(checker.Contains), "Retrying")
}

func getTestTokenService(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func (s *DockerRegistryAuthTokenSuite) TestPushTokenServiceUnauthResponse(c *check.C) {
	ts := getTestTokenService(http.StatusUnauthorized, `{"errors": [{"Code":"UNAUTHORIZED", "message": "a message", "detail": null}]}`)
	defer ts.Close()
	s.setupRegistryWithTokenService(c, ts.URL)
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, checker.Not(checker.Contains), "Retrying")
	c.Assert(out, checker.Contains, "unauthorized: a message")
}

func (s *DockerRegistryAuthTokenSuite) TestPushMisconfiguredTokenServiceResponseUnauthorized(c *check.C) {
	ts := getTestTokenService(http.StatusUnauthorized, `{"error": "unauthorized"}`)
	defer ts.Close()
	s.setupRegistryWithTokenService(c, ts.URL)
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, checker.Not(checker.Contains), "Retrying")
	split := strings.Split(out, "\n")
	c.Assert(split[len(split)-2], check.Equals, "unauthorized: authentication required")
}

func (s *DockerRegistryAuthTokenSuite) TestPushMisconfiguredTokenServiceResponseError(c *check.C) {
	ts := getTestTokenService(http.StatusInternalServerError, `{"error": "unexpected"}`)
	defer ts.Close()
	s.setupRegistryWithTokenService(c, ts.URL)
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, checker.Contains, "Retrying")
	split := strings.Split(out, "\n")
	c.Assert(split[len(split)-2], check.Equals, "received unexpected HTTP status: 500 Internal Server Error")
}

func (s *DockerRegistryAuthTokenSuite) TestPushMisconfiguredTokenServiceResponseUnparsable(c *check.C) {
	ts := getTestTokenService(http.StatusForbidden, `no way`)
	defer ts.Close()
	s.setupRegistryWithTokenService(c, ts.URL)
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, checker.Not(checker.Contains), "Retrying")
	split := strings.Split(out, "\n")
	c.Assert(split[len(split)-2], checker.Contains, "error parsing HTTP 403 response body: ")
}

func (s *DockerRegistryAuthTokenSuite) TestPushMisconfiguredTokenServiceResponseNoToken(c *check.C) {
	ts := getTestTokenService(http.StatusOK, `{"something": "wrong"}`)
	defer ts.Close()
	s.setupRegistryWithTokenService(c, ts.URL)
	repoName := fmt.Sprintf("%s/busybox", privateRegistryURL)
	dockerCmd(c, "tag", "busybox", repoName)
	out, _, err := dockerCmdWithError("push", repoName)
	c.Assert(err, check.NotNil, check.Commentf(out))
	c.Assert(out, checker.Not(checker.Contains), "Retrying")
	split := strings.Split(out, "\n")
	c.Assert(split[len(split)-2], check.Equals, "authorization server did not include a token in the response")
}

func (s *DockerSuite) TestPushOfficialImage(c *check.C) {
	var reErr = regexp.MustCompile(`rename your repository to[^:]*:\s*docker\.io/<user>/busybox\b`)

	// push busybox to public registry as "library/busybox"
	cmd := exec.Command(dockerBinary, "push", "library/busybox")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.Fatalf("Failed to get stdout pipe for process: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.Fatalf("Failed to get stderr pipe for process: %v", err)
	}
	if err := cmd.Start(); err != nil {
		c.Fatalf("Failed to start pushing to public registry: %v", err)
	}
	outReader := bufio.NewReader(stdout)
	errReader := bufio.NewReader(stderr)
	line, isPrefix, err := errReader.ReadLine()
	if err != nil {
		c.Fatalf("Failed to read farewell: %v", err)
	}
	if isPrefix {
		c.Fatalf("Got unexpectedly long output.")
	}
	if !reErr.Match(line) {
		c.Fatalf("Got unexpected output %q", line)
	}
	if line, _, err = outReader.ReadLine(); err != io.EOF {
		c.Fatalf("Expected EOF, not: %q", line)
	}
	for ; err != io.EOF; line, _, err = errReader.ReadLine() {
		c.Fatalf("Expected no message on stderr, got: %q", string(line))
	}

	// Wait for command to finish with short timeout.
	finish := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err == nil {
			c.Error("Push command should have failed.")
		}
		close(finish)
	}()
	select {
	case <-finish:
	case <-time.After(1 * time.Second):
		c.Fatalf("Docker push failed to exit.")
	}
}

func (s *DockerRegistrySuite) TestPushToAdditionalRegistry(c *check.C) {
	if err := s.d.StartWithBusybox("--add-registry=" + s.reg.url); err != nil {
		c.Fatalf("we should have been able to start the daemon with passing add-registry=%s: %v", s.reg.url, err)
	}

	bbImg := s.d.getAndTestImageEntry(c, 1, "busybox", "")

	// push busybox to additional registry as "library/busybox" and remove all local images
	if out, err := s.d.Cmd("tag", "busybox", "library/busybox"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("push", "library/busybox"); err != nil {
		c.Fatalf("failed to push image library/busybox: error %v, output %q", err, out)
	}
	toRemove := []string{"busybox", "library/busybox"}
	if out, err := s.d.Cmd("rmi", toRemove...); err != nil {
		c.Fatalf("failed to remove images %v: %v, output: %s", toRemove, err, out)
	}
	s.d.getAndTestImageEntry(c, 0, "", "")

	// pull it from additional registry
	if _, err := s.d.Cmd("pull", "library/busybox"); err != nil {
		c.Fatalf("we should have been able to pull library/busybox from %q: %v", s.reg.url, err)
	}
	bb2Img := s.d.getAndTestImageEntry(c, 1, s.reg.url+"/library/busybox", "")
	if bb2Img.size != bbImg.size {
		c.Fatalf("expected %s and %s to have the same size (%s != %s)", bb2Img.name, bbImg.name, bb2Img.size, bbImg.size)
	}
}

func (s *DockerRegistrySuite) TestPushCustomTagToAdditionalRegistry(c *check.C) {
	if err := s.d.StartWithBusybox("--add-registry=" + s.reg.url); err != nil {
		c.Fatalf("we should have been able to start the daemon with passing add-registry=%s: %v", s.reg.url, err)
	}

	busyboxID := s.d.getAndTestImageEntry(c, 1, "busybox", "").id

	if out, err := s.d.Cmd("tag", "busybox", "user/busybox:1.2.3"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("tag", "busybox", s.reg.url+"/user/busybox:latest"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("push", "user/busybox:1.2.3"); err != nil {
		c.Fatalf("failed to push image user/busybox: error %v, output %q", err, out)
	}
	s.d.getAndTestImageEntry(c, 3, "user/busybox", busyboxID)
	toRemove := []string{"user/busybox:1.2.3"}
	if out, err := s.d.Cmd("rmi", toRemove...); err != nil {
		c.Fatalf("failed to remove images %v: %v, output: %s", toRemove, err, out)
	}
	s.d.getAndTestImageEntry(c, 2, s.reg.url+"/user/busybox", busyboxID)
}

func (s *DockerRegistriesSuite) TestPushNeedsAuth(c *check.C) {
	c.Assert(s.d.StartWithBusybox("--add-registry="+s.regWithAuth.url), check.IsNil)

	repo := fmt.Sprintf("%s/runcom/busybox", s.regWithAuth.url)
	repoUnqualified := "runcom/busybox"

	out, err := s.d.Cmd("tag", "busybox", repoUnqualified)
	c.Assert(err, check.IsNil, check.Commentf(out))

	// this means it needs auth...
	resp, err := http.Get(fmt.Sprintf("http://%s/v2/", s.regWithAuth.url))
	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusUnauthorized)

	// login with the registry...
	out, err = s.d.Cmd("login", "-u", s.regWithAuth.username, "-p", s.regWithAuth.password, "-e", s.regWithAuth.email, s.regWithAuth.url)
	c.Assert(err, check.IsNil, check.Commentf(out))

	// push to private registry with unqualified image name
	out, err = s.d.Cmd("push", repoUnqualified)
	c.Assert(err, check.IsNil, check.Commentf(out))

	// remove the repo locally
	out, err = s.d.Cmd("rmi", "-f", repoUnqualified)
	c.Assert(err, check.IsNil, check.Commentf(out))

	// pull the image from the private registry so that we're sure it was pushed there
	out, err = s.d.Cmd("pull", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))
	expected := fmt.Sprintf("Pulling from %s", repo)
	if !strings.Contains(out, expected) {
		c.Fatalf("Wanted %s, got %s", expected, out)
	}
}

func (s *DockerRegistrySuite) TestPushWithSkipSchema2(c *check.C) {
	c.Assert(s.d.StartWithBusybox("--skip-schema2-push=false"), check.IsNil)

	repo := fmt.Sprintf("%s/runcom/busybox", s.reg.url)
	out, err := s.d.Cmd("tag", "busybox", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("push", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))

	digest1 := reference.DigestRegexp.FindString(out)
	c.Assert(len(digest1), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))

	out, err = s.d.Cmd("pull", repo+"@"+digest1)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("rmi", "-f", repo+"@"+digest1)
	c.Assert(err, check.IsNil, check.Commentf(out))

	c.Assert(s.d.Restart("--skip-schema2-push=true"), check.IsNil)

	out, err = s.d.Cmd("push", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))

	digest2 := reference.DigestRegexp.FindString(out)
	c.Assert(len(digest2), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))

	out, err = s.d.Cmd("pull", repo+"@"+digest2)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("rmi", "-f", repo+"@"+digest2)
	c.Assert(err, check.IsNil, check.Commentf(out))

	c.Assert(digest1, check.Not(checker.Equals), digest2)

	c.Assert(s.d.Restart(), check.IsNil)

	out, err = s.d.Cmd("push", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))

	digest3 := reference.DigestRegexp.FindString(out)
	c.Assert(len(digest3), checker.GreaterThan, 0, check.Commentf("no digest found for pushed manifest"))

	out, err = s.d.Cmd("pull", repo+"@"+digest3)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("rmi", "-f", repo+"@"+digest3)
	c.Assert(err, check.IsNil, check.Commentf(out))

	c.Assert(digest1, checker.Equals, digest3)
}
