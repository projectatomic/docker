package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/docker/integration-cli/checker"
	"github.com/docker/docker/integration-cli/daemon"
	icmd "github.com/docker/docker/pkg/testutil/cmd"
	"github.com/go-check/check"
	"github.com/opencontainers/go-digest"
)

// testPullImageWithAliases pulls a specific image tag and verifies that any aliases (i.e., other
// tags for the same image) are not also pulled down.
//
// Ref: docker/docker#8141
func testPullImageWithAliases(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)

	repos := []string{}
	for _, tag := range []string{"recent", "fresh"} {
		repos = append(repos, fmt.Sprintf("%v:%v", repoName, tag))
	}

	// Tag and push the same image multiple times.
	for _, repo := range repos {
		dockerCmd(c, "tag", "busybox", repo)
		dockerCmd(c, "push", repo)
	}

	// Clear local images store.
	args := append([]string{"rmi"}, repos...)
	dockerCmd(c, args...)

	// Pull a single tag and verify it doesn't bring down all aliases.
	dockerCmd(c, "pull", repos[0])
	dockerCmd(c, "inspect", repos[0])
	for _, repo := range repos[1:] {
		_, _, err := dockerCmdWithError("inspect", repo)
		c.Assert(err, checker.NotNil, check.Commentf("Image %v shouldn't have been pulled down", repo))
	}
}

func (s *DockerRegistrySuite) TestPullImageWithAliases(c *check.C) {
	testPullImageWithAliases(c)
}

func (s *DockerSchema1RegistrySuite) TestPullImageWithAliases(c *check.C) {
	testPullImageWithAliases(c)
}

// testConcurrentPullWholeRepo pulls the same repo concurrently.
func testConcurrentPullWholeRepo(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)

	repos := []string{}
	for _, tag := range []string{"recent", "fresh", "todays"} {
		repo := fmt.Sprintf("%v:%v", repoName, tag)
		buildImageSuccessfully(c, repo, withDockerfile(fmt.Sprintf(`
		    FROM busybox
		    ENTRYPOINT ["/bin/echo"]
		    ENV FOO foo
		    ENV BAR bar
		    CMD echo %s
		`, repo)))
		dockerCmd(c, "push", repo)
		repos = append(repos, repo)
	}

	// Clear local images store.
	args := append([]string{"rmi"}, repos...)
	dockerCmd(c, args...)

	// Run multiple re-pulls concurrently
	results := make(chan error)
	numPulls := 3

	for i := 0; i != numPulls; i++ {
		go func() {
			result := icmd.RunCommand(dockerBinary, "pull", "-a", repoName)
			results <- result.Error
		}()
	}

	// These checks are separate from the loop above because the check
	// package is not goroutine-safe.
	for i := 0; i != numPulls; i++ {
		err := <-results
		c.Assert(err, checker.IsNil, check.Commentf("concurrent pull failed with error: %v", err))
	}

	// Ensure all tags were pulled successfully
	for _, repo := range repos {
		dockerCmd(c, "inspect", repo)
		out, _ := dockerCmd(c, "run", "--rm", repo)
		c.Assert(strings.TrimSpace(out), checker.Equals, "/bin/sh -c echo "+repo)
	}
}

func (s *DockerRegistrySuite) testConcurrentPullWholeRepo(c *check.C) {
	testConcurrentPullWholeRepo(c)
}

func (s *DockerSchema1RegistrySuite) testConcurrentPullWholeRepo(c *check.C) {
	testConcurrentPullWholeRepo(c)
}

// testConcurrentFailingPull tries a concurrent pull that doesn't succeed.
func testConcurrentFailingPull(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)

	// Run multiple pulls concurrently
	results := make(chan error)
	numPulls := 3

	for i := 0; i != numPulls; i++ {
		go func() {
			result := icmd.RunCommand(dockerBinary, "pull", repoName+":asdfasdf")
			results <- result.Error
		}()
	}

	// These checks are separate from the loop above because the check
	// package is not goroutine-safe.
	for i := 0; i != numPulls; i++ {
		err := <-results
		c.Assert(err, checker.NotNil, check.Commentf("expected pull to fail"))
	}
}

func (s *DockerRegistrySuite) testConcurrentFailingPull(c *check.C) {
	testConcurrentFailingPull(c)
}

func (s *DockerSchema1RegistrySuite) testConcurrentFailingPull(c *check.C) {
	testConcurrentFailingPull(c)
}

// testConcurrentPullMultipleTags pulls multiple tags from the same repo
// concurrently.
func testConcurrentPullMultipleTags(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)

	repos := []string{}
	for _, tag := range []string{"recent", "fresh", "todays"} {
		repo := fmt.Sprintf("%v:%v", repoName, tag)
		buildImageSuccessfully(c, repo, withDockerfile(fmt.Sprintf(`
		    FROM busybox
		    ENTRYPOINT ["/bin/echo"]
		    ENV FOO foo
		    ENV BAR bar
		    CMD echo %s
		`, repo)))
		dockerCmd(c, "push", repo)
		repos = append(repos, repo)
	}

	// Clear local images store.
	args := append([]string{"rmi"}, repos...)
	dockerCmd(c, args...)

	// Re-pull individual tags, in parallel
	results := make(chan error)

	for _, repo := range repos {
		go func(repo string) {
			result := icmd.RunCommand(dockerBinary, "pull", repo)
			results <- result.Error
		}(repo)
	}

	// These checks are separate from the loop above because the check
	// package is not goroutine-safe.
	for range repos {
		err := <-results
		c.Assert(err, checker.IsNil, check.Commentf("concurrent pull failed with error: %v", err))
	}

	// Ensure all tags were pulled successfully
	for _, repo := range repos {
		dockerCmd(c, "inspect", repo)
		out, _ := dockerCmd(c, "run", "--rm", repo)
		c.Assert(strings.TrimSpace(out), checker.Equals, "/bin/sh -c echo "+repo)
	}
}

func (s *DockerRegistrySuite) TestConcurrentPullMultipleTags(c *check.C) {
	testConcurrentPullMultipleTags(c)
}

func (s *DockerSchema1RegistrySuite) TestConcurrentPullMultipleTags(c *check.C) {
	testConcurrentPullMultipleTags(c)
}

// testPullIDStability verifies that pushing an image and pulling it back
// preserves the image ID.
func testPullIDStability(c *check.C) {
	derivedImage := privateRegistryURL + "/dockercli/id-stability"
	baseImage := "busybox"

	buildImageSuccessfully(c, derivedImage, withDockerfile(fmt.Sprintf(`
	    FROM %s
	    ENV derived true
	    ENV asdf true
	    RUN dd if=/dev/zero of=/file bs=1024 count=1024
	    CMD echo %s
	`, baseImage, derivedImage)))

	originalID := getIDByName(c, derivedImage)
	dockerCmd(c, "push", derivedImage)

	// Pull
	out, _ := dockerCmd(c, "pull", derivedImage)
	if strings.Contains(out, "Pull complete") {
		c.Fatalf("repull redownloaded a layer: %s", out)
	}

	derivedIDAfterPull := getIDByName(c, derivedImage)

	if derivedIDAfterPull != originalID {
		c.Fatal("image's ID unexpectedly changed after a repush/repull")
	}

	// Make sure the image runs correctly
	out, _ = dockerCmd(c, "run", "--rm", derivedImage)
	if strings.TrimSpace(out) != derivedImage {
		c.Fatalf("expected %s; got %s", derivedImage, out)
	}

	// Confirm that repushing and repulling does not change the computed ID
	dockerCmd(c, "push", derivedImage)
	dockerCmd(c, "rmi", derivedImage)
	dockerCmd(c, "pull", derivedImage)

	derivedIDAfterPull = getIDByName(c, derivedImage)

	if derivedIDAfterPull != originalID {
		c.Fatal("image's ID unexpectedly changed after a repush/repull")
	}

	// Make sure the image still runs
	out, _ = dockerCmd(c, "run", "--rm", derivedImage)
	if strings.TrimSpace(out) != derivedImage {
		c.Fatalf("expected %s; got %s", derivedImage, out)
	}
}

func (s *DockerRegistrySuite) TestPullIDStability(c *check.C) {
	testPullIDStability(c)
}

func (s *DockerSchema1RegistrySuite) TestPullIDStability(c *check.C) {
	testPullIDStability(c)
}

// #21213
func testPullNoLayers(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/scratch", privateRegistryURL)

	buildImageSuccessfully(c, repoName, withDockerfile(`
	FROM scratch
	ENV foo bar`))
	dockerCmd(c, "push", repoName)
	dockerCmd(c, "rmi", repoName)
	dockerCmd(c, "pull", repoName)
}

func (s *DockerRegistrySuite) TestPullNoLayers(c *check.C) {
	testPullNoLayers(c)
}

func (s *DockerSchema1RegistrySuite) TestPullNoLayers(c *check.C) {
	testPullNoLayers(c)
}

func (s *DockerRegistrySuite) TestPullManifestList(c *check.C) {
	testRequires(c, NotArm)
	pushDigest, err := setupImage(c)
	c.Assert(err, checker.IsNil, check.Commentf("error setting up image"))

	// Inject a manifest list into the registry
	manifestList := &manifestlist.ManifestList{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     manifestlist.MediaTypeManifestList,
		},
		Manifests: []manifestlist.ManifestDescriptor{
			{
				Descriptor: distribution.Descriptor{
					Digest:    "sha256:1a9ec845ee94c202b2d5da74a24f0ed2058318bfa9879fa541efaecba272e86b",
					Size:      3253,
					MediaType: schema2.MediaTypeManifest,
				},
				Platform: manifestlist.PlatformSpec{
					Architecture: "bogus_arch",
					OS:           "bogus_os",
				},
			},
			{
				Descriptor: distribution.Descriptor{
					Digest:    pushDigest,
					Size:      3253,
					MediaType: schema2.MediaTypeManifest,
				},
				Platform: manifestlist.PlatformSpec{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
			},
		},
	}

	manifestListJSON, err := json.MarshalIndent(manifestList, "", "   ")
	c.Assert(err, checker.IsNil, check.Commentf("error marshalling manifest list"))

	manifestListDigest := digest.FromBytes(manifestListJSON)
	hexDigest := manifestListDigest.Hex()

	registryV2Path := s.reg.Path()

	// Write manifest list to blob store
	blobDir := filepath.Join(registryV2Path, "blobs", "sha256", hexDigest[:2], hexDigest)
	err = os.MkdirAll(blobDir, 0755)
	c.Assert(err, checker.IsNil, check.Commentf("error creating blob dir"))
	blobPath := filepath.Join(blobDir, "data")
	err = ioutil.WriteFile(blobPath, []byte(manifestListJSON), 0644)
	c.Assert(err, checker.IsNil, check.Commentf("error writing manifest list"))

	// Add to revision store
	revisionDir := filepath.Join(registryV2Path, "repositories", remoteRepoName, "_manifests", "revisions", "sha256", hexDigest)
	err = os.Mkdir(revisionDir, 0755)
	c.Assert(err, checker.IsNil, check.Commentf("error creating revision dir"))
	revisionPath := filepath.Join(revisionDir, "link")
	err = ioutil.WriteFile(revisionPath, []byte(manifestListDigest.String()), 0644)
	c.Assert(err, checker.IsNil, check.Commentf("error writing revision link"))

	// Update tag
	tagPath := filepath.Join(registryV2Path, "repositories", remoteRepoName, "_manifests", "tags", "latest", "current", "link")
	err = ioutil.WriteFile(tagPath, []byte(manifestListDigest.String()), 0644)
	c.Assert(err, checker.IsNil, check.Commentf("error writing tag link"))

	// Verify that the image can be pulled through the manifest list.
	out, _ := dockerCmd(c, "pull", repoName)

	// The pull output includes "Digest: <digest>", so find that
	matches := digestRegex.FindStringSubmatch(out)
	c.Assert(matches, checker.HasLen, 2, check.Commentf("unable to parse digest from pull output: %s", out))
	pullDigest := matches[1]

	// Make sure the pushed and pull digests match
	c.Assert(manifestListDigest.String(), checker.Equals, pullDigest)

	// Was the image actually created?
	dockerCmd(c, "inspect", repoName)

	dockerCmd(c, "rmi", repoName)
}

// #23100
func (s *DockerRegistryAuthHtpasswdSuite) TestPullWithExternalAuthLoginWithScheme(c *check.C) {
	osPath := os.Getenv("PATH")
	defer os.Setenv("PATH", osPath)

	workingDir, err := os.Getwd()
	c.Assert(err, checker.IsNil)
	absolute, err := filepath.Abs(filepath.Join(workingDir, "fixtures", "auth"))
	c.Assert(err, checker.IsNil)
	testPath := fmt.Sprintf("%s%c%s", osPath, filepath.ListSeparator, absolute)

	os.Setenv("PATH", testPath)

	repoName := fmt.Sprintf("%v/dockercli/busybox:authtest", privateRegistryURL)

	tmp, err := ioutil.TempDir("", "integration-cli-")
	c.Assert(err, checker.IsNil)

	externalAuthConfig := `{ "credsStore": "shell-test" }`

	configPath := filepath.Join(tmp, "config.json")
	err = ioutil.WriteFile(configPath, []byte(externalAuthConfig), 0644)
	c.Assert(err, checker.IsNil)

	dockerCmd(c, "--config", tmp, "login", "-u", s.reg.Username(), "-p", s.reg.Password(), privateRegistryURL)

	b, err := ioutil.ReadFile(configPath)
	c.Assert(err, checker.IsNil)
	c.Assert(string(b), checker.Not(checker.Contains), "\"auth\":")

	dockerCmd(c, "--config", tmp, "tag", "busybox", repoName)
	dockerCmd(c, "--config", tmp, "push", repoName)

	dockerCmd(c, "--config", tmp, "logout", privateRegistryURL)
	dockerCmd(c, "--config", tmp, "login", "-u", s.reg.Username(), "-p", s.reg.Password(), "https://"+privateRegistryURL)
	dockerCmd(c, "--config", tmp, "pull", repoName)

	// likewise push should work
	repoName2 := fmt.Sprintf("%v/dockercli/busybox:nocreds", privateRegistryURL)
	dockerCmd(c, "tag", repoName, repoName2)
	dockerCmd(c, "--config", tmp, "push", repoName2)

	// logout should work w scheme also because it will be stripped
	dockerCmd(c, "--config", tmp, "logout", "https://"+privateRegistryURL)
}

func (s *DockerRegistryAuthHtpasswdSuite) TestPullWithExternalAuth(c *check.C) {
	osPath := os.Getenv("PATH")
	defer os.Setenv("PATH", osPath)

	workingDir, err := os.Getwd()
	c.Assert(err, checker.IsNil)
	absolute, err := filepath.Abs(filepath.Join(workingDir, "fixtures", "auth"))
	c.Assert(err, checker.IsNil)
	testPath := fmt.Sprintf("%s%c%s", osPath, filepath.ListSeparator, absolute)

	os.Setenv("PATH", testPath)

	repoName := fmt.Sprintf("%v/dockercli/busybox:authtest", privateRegistryURL)

	tmp, err := ioutil.TempDir("", "integration-cli-")
	c.Assert(err, checker.IsNil)

	externalAuthConfig := `{ "credsStore": "shell-test" }`

	configPath := filepath.Join(tmp, "config.json")
	err = ioutil.WriteFile(configPath, []byte(externalAuthConfig), 0644)
	c.Assert(err, checker.IsNil)

	dockerCmd(c, "--config", tmp, "login", "-u", s.reg.Username(), "-p", s.reg.Password(), privateRegistryURL)

	b, err := ioutil.ReadFile(configPath)
	c.Assert(err, checker.IsNil)
	c.Assert(string(b), checker.Not(checker.Contains), "\"auth\":")

	dockerCmd(c, "--config", tmp, "tag", "busybox", repoName)
	dockerCmd(c, "--config", tmp, "push", repoName)

	dockerCmd(c, "--config", tmp, "pull", repoName)
}

// TestRunImplicitPullWithNoTag should pull implicitly only the default tag (latest)
func (s *DockerRegistrySuite) TestRunImplicitPullWithNoTag(c *check.C) {
	testRequires(c, DaemonIsLinux)
	repo := fmt.Sprintf("%v/dockercli/busybox", privateRegistryURL)
	repoTag1 := fmt.Sprintf("%v:latest", repo)
	repoTag2 := fmt.Sprintf("%v:t1", repo)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoTag1)
	dockerCmd(c, "tag", "busybox", repoTag2)
	dockerCmd(c, "push", repo)
	dockerCmd(c, "rmi", repoTag1)
	dockerCmd(c, "rmi", repoTag2)

	out, _ := dockerCmd(c, "run", repo)
	c.Assert(out, checker.Contains, fmt.Sprintf("Unable to find image '%s:latest' locally", repo))

	// There should be only one line for repo, the one with repo:latest
	outImageCmd, _ := dockerCmd(c, "images", repo)
	splitOutImageCmd := strings.Split(strings.TrimSpace(outImageCmd), "\n")
	c.Assert(splitOutImageCmd, checker.HasLen, 2)
}

// Test pulls from blocked public registry and from private registry. This
// shall be called with various daemonArgs containing at least one
// `--block-registry` flag.
func (s *DockerRegistrySuite) doTestPullFromBlockedPublicRegistry(c *check.C, daemonArgs []string) {
	allBlocked := false
	for _, arg := range daemonArgs {
		if arg == "--block-registry=all" {
			allBlocked = true
		}
	}
	s.d.StartWithBusybox(c, daemonArgs...)

	busyboxID := s.d.GetAndTestImageEntry(c, 1, "busybox", "").Id

	// try to pull from docker.io
	if out, err := s.d.Cmd("pull", "library/hello-world"); err == nil {
		c.Fatalf("pull from blocked public registry should have failed, output: %s", out)
	}

	// tag busybox as library/hello-world and push it to some private registry
	if out, err := s.d.Cmd("tag", "busybox", s.reg.URL()+"/library/hello-world"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("push", s.reg.URL()+"/library/hello-world"); !allBlocked && err != nil {
		c.Fatalf("failed to push image %s: error %v, output %q", s.reg.URL()+"/library/hello-world", err, out)
	} else if allBlocked && err == nil {
		c.Fatalf("push to private registry should have failed, output: %q", out)
	}

	// remove library/hello-world image
	if out, err := s.d.Cmd("rmi", s.reg.URL()+"/library/hello-world"); err != nil {
		c.Fatalf("failed to remove images %v: %v, output: %s", s.reg.URL()+"/library/hello-world", err, out)
	}
	s.d.GetAndTestImageEntry(c, 1, "busybox", busyboxID)

	// try to pull from private registry
	if out, err := s.d.Cmd("pull", s.reg.URL()+"/library/hello-world"); !allBlocked && err != nil {
		c.Fatalf("we should have been able to pull %s/library/hello-world: %v", s.reg.URL(), err)
	} else if allBlocked && err == nil {
		c.Fatalf("pull from private registry should have failed, output: %q", out)
	} else if !allBlocked {
		s.d.GetAndTestImageEntry(c, 2, s.reg.URL()+"/library/hello-world", busyboxID)
	}
}

func (s *DockerRegistrySuite) TestPullFromBlockedPublicRegistry(c *check.C) {
	for _, blockedRegistry := range []string{"public", "docker.io"} {
		s.doTestPullFromBlockedPublicRegistry(c, []string{"--block-registry=" + blockedRegistry})
		s.d.Stop(c)
		s.d = daemon.New(c, dockerBinary, dockerdBinary, daemon.Config{
			Experimental: testEnv.ExperimentalDaemon(),
		})
	}
}

func (s *DockerRegistrySuite) TestPullWithAllRegistriesBlocked(c *check.C) {
	s.doTestPullFromBlockedPublicRegistry(c, []string{"--block-registry=all"})
}

// Test pulls from additional registry with public registry blocked. This
// shall be called with various daemonArgs containing at least one
// `--block-registry` flag.
func (s *DockerRegistriesSuite) doTestPullFromPrivateRegistriesWithPublicBlocked(c *check.C, daemonArgs []string) {
	allBlocked := false
	for _, arg := range daemonArgs {
		if arg == "--block-registry=all" {
			allBlocked = true
		}
	}
	daemonArgs = append(daemonArgs, "--add-registry="+s.reg1.URL())
	s.d.StartWithBusybox(c, daemonArgs...)

	bbImg := s.d.GetAndTestImageEntry(c, 1, "busybox", "")

	// try to pull from blocked public registry
	if out, err := s.d.Cmd("pull", "library/hello-world"); err == nil {
		c.Fatalf("pulling from blocked public registry should have failed, output: %s", out)
	}

	// push busybox to
	//  additional registry as "misc/busybox"
	//  private registry as "library/busybox"
	// and remove all local images
	if out, err := s.d.Cmd("tag", "busybox", s.reg1.URL()+"/misc/busybox"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("tag", "busybox", s.reg2.URL()+"/library/busybox"); err != nil {
		c.Fatalf("failed to tag image %s: error %v, output %q", "busybox", err, out)
	}
	if out, err := s.d.Cmd("push", s.reg1.URL()+"/misc/busybox"); err != nil {
		c.Fatalf("failed to push image %s: error %v, output %q", s.reg1.URL()+"/misc/busybox", err, out)
	}
	if out, err := s.d.Cmd("push", s.reg2.URL()+"/library/busybox"); !allBlocked && err != nil {
		c.Fatalf("failed to push image %s: error %v, output %q", s.reg2.URL()+"/library/busybox", err, out)
	} else if allBlocked && err == nil {
		c.Fatalf("push to private registry should have failed, output: %q", out)
	}
	toRemove := []string{"rmi", "busybox", "misc/busybox", s.reg2.URL() + "/library/busybox"}
	if out, err := s.d.Cmd(toRemove...); err != nil {
		c.Fatalf("failed to remove images %v: %v, output: %s", toRemove, err, out)
	}
	s.d.GetAndTestImageEntry(c, 0, "", "")

	// try to pull "library/busybox" from additional registry
	if out, err := s.d.Cmd("pull", "library/busybox"); err == nil {
		c.Fatalf("pull of library/busybox from additional registry should have failed, output: %q", out)
	}

	// now pull the "misc/busybox" from additional registry
	if _, err := s.d.Cmd("pull", "misc/busybox"); err != nil {
		c.Fatalf("we should have been able to pull misc/hello-world from %q: %v", s.reg1.URL(), err)
	}
	bb2Img := s.d.GetAndTestImageEntry(c, 1, s.reg1.URL()+"/misc/busybox", "")
	if bb2Img.Size != bbImg.Size {
		c.Fatalf("expected %s and %s to have the same size (%s != %s)", bb2Img.Name, bbImg.Name, bb2Img.Size, bbImg.Size)
	}

	// try to pull "library/busybox" from private registry
	if out, err := s.d.Cmd("pull", s.reg2.URL()+"/library/busybox"); !allBlocked && err != nil {
		c.Fatalf("we should have been able to pull %s/library/busybox: %v", s.reg2.URL(), err)
	} else if allBlocked && err == nil {
		c.Fatalf("pull from private registry should have failed, output: %q", out)
	} else if !allBlocked {
		bb3Img := s.d.GetAndTestImageEntry(c, 2, s.reg2.URL()+"/library/busybox", "")
		if bb3Img.Size != bbImg.Size {
			c.Fatalf("expected %s and %s to have the same size (%s != %s)", bb3Img.Name, bbImg.Name, bb3Img.Size, bbImg.Size)
		}
	}
}

func (s *DockerRegistriesSuite) TestPullFromPrivateRegistriesWithPublicBlocked(c *check.C) {
	for _, blockedRegistry := range []string{"public", "docker.io"} {
		s.doTestPullFromPrivateRegistriesWithPublicBlocked(c, []string{"--block-registry=" + blockedRegistry})
		s.d.Stop(c)
		s.d = daemon.New(c, dockerBinary, dockerdBinary, daemon.Config{
			Experimental: testEnv.ExperimentalDaemon(),
		})
	}
}

func (s *DockerRegistriesSuite) TestPullFromAdditionalRegistryWithAllBlocked(c *check.C) {
	s.doTestPullFromPrivateRegistriesWithPublicBlocked(c, []string{"--block-registry=all"})
}

func (s *DockerRegistriesSuite) setupSignedImages(c *check.C) {
	cmd := exec.Command("mkdir", "/root/.gnupg")
	out, _, err := runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))

	cmd = exec.Command("gpg", "--batch", "--gen-key")
	cmd.Stdin = strings.NewReader(`Key-Type: RSA
Name-Real: Personal signing key
Name-Email: test@redhat.com
%commit`)
	out, _, err = runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))

	cmd = exec.Command("gpg", "--armor", "--export", "--output", "/root/personal-pubkey.gpg", "test@redhat.com")
	out, _, err = runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))

	tmp, err := ioutil.TempDir("", "sigstore-")
	c.Assert(err, checker.IsNil)
	sigstorePath := filepath.Join(tmp, "sigstore")

	registriesd := fmt.Sprintf(`default-docker:
   sigstore: file://%s
   sigstore-staging: file://%s
`, sigstorePath, sigstorePath)
	c.Assert(ioutil.WriteFile("/etc/containers/registries.d/default.yaml", []byte(registriesd), 0755), check.IsNil)

	// signed image
	cmd = exec.Command("skopeo", "--tls-verify=false", "copy", "--sign-by", "test@redhat.com", "docker://busybox", "docker://"+s.reg1.URL()+"/busybox:signed")
	out, _, err = runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))

	// unsigned image on reg1
	cmd = exec.Command("skopeo", "--tls-verify=false", "copy", "docker://busybox:1-glibc", "docker://"+s.reg1.URL()+"/busybox:what")
	out, _, err = runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))

	// unsigned image on reg2
	cmd = exec.Command("skopeo", "--tls-verify=false", "copy", "docker://alpine", "docker://"+s.reg2.URL()+"/busybox:reg2")
	out, _, err = runCommandWithOutput(cmd)
	c.Assert(err, checker.IsNil, check.Commentf(out))
}

func (s *DockerRegistriesSuite) TestPullWithPolicy(c *check.C) {
	testRequires(c, DaemonIsLinux)
	testRequires(c, Network)

	homePath := os.Getenv("HOME")
	defer os.Setenv("HOME", homePath)
	os.Setenv("HOME", "/root")

	s.setupSignedImages(c)

	defer func() {
		defaultPolicy := `{
	"default": [
		{
			"type": "insecureAcceptAnything"
		}
	]
}`
		c.Assert(ioutil.WriteFile("/etc/containers/policy.json", []byte(defaultPolicy), 0755), check.IsNil)
	}()

	// test pull default policy allow anything
	dockerCmd(c, "pull", "alpine")
	out, _ := dockerCmd(c, "pull", s.reg1.URL()+"/busybox:what")
	matches := digestRegex.FindStringSubmatch(out)
	c.Assert(matches, checker.HasLen, 2, check.Commentf("unable to parse digest from pull output: %s", out))
	unsignedDigest := matches[1]

	// test pull default policy reject
	rejectPolicy := `{
	"default": [
		{
			"type": "reject"
		}
	]
}`
	c.Assert(ioutil.WriteFile("/etc/containers/policy.json", []byte(rejectPolicy), 0755), check.IsNil)

	out, _, err := dockerCmdWithError("pull", "fedora")
	c.Assert(err, checker.NotNil)
	c.Assert(string(out), checker.Contains, "is rejected by policy.")

	// test pull default policy reject + allow private reg with gpg signed
	policy1 := fmt.Sprintf(`{
	"default": [
		{
			"type": "reject"
		}
	],
	"transports": {
		"docker": {
			"%s/busybox": [
			{
				"type": "signedBy",
				"keyType": "GPGKeys",
				"keyPath": "/root/personal-pubkey.gpg"
			}
			]
		}
	}
}`, s.reg1.URL())
	c.Assert(ioutil.WriteFile("/etc/containers/policy.json", []byte(policy1), 0755), check.IsNil)

	// verification passes for the correct signed image
	out, _ = dockerCmd(c, "pull", s.reg1.URL()+"/busybox:signed")
	matches = digestRegex.FindStringSubmatch(out)
	c.Assert(matches, checker.HasLen, 2, check.Commentf("unable to parse digest from pull output: %s", out))
	dgst := matches[1]

	// test that I can also pull by digest when the signature is valid for the tag
	dgstRef := fmt.Sprintf("%s/busybox@%s", s.reg1.URL(), dgst)
	dockerCmd(c, "pull", dgstRef)

	dgstRefUnsigned := fmt.Sprintf("%s/busybox@%s", s.reg1.URL(), unsignedDigest)
	out, _, err = dockerCmdWithError("pull", dgstRefUnsigned)
	c.Assert(err, check.NotNil)
	c.Assert(out, checker.Contains, "no signature exists")

	// test that I cannot pull by digest when the signature is valid for the tag
	policy2 := fmt.Sprintf(`{
	"default": [
		{
			"type": "reject"
		}
	],
	"transports": {
		"docker": {
			"%s/busybox": [
			{
				"signedIdentity": {
					"type": "matchExact"
				},
				"type": "signedBy",
				"keyType": "GPGKeys",
				"keyPath": "/root/personal-pubkey.gpg"
			}
			]
		}
	}
}`, s.reg1.URL())
	c.Assert(ioutil.WriteFile("/etc/containers/policy.json", []byte(policy2), 0755), check.IsNil)
	out, _, err = dockerCmdWithError("pull", dgstRef)
	c.Assert(err, check.NotNil)
	c.Assert(out, checker.Contains, "is not accepted")

	// test pull default policy reject + allow anything from reg2
	policy3 := fmt.Sprintf(`{
	"default": [
		{
			"type": "reject"
		}
	],
	"transports": {
		"docker": {
			"%s": [
			{
				"type": "insecureAcceptAnything"
			}
			]
		}
	}
}`, s.reg2.URL())
	c.Assert(ioutil.WriteFile("/etc/containers/policy.json", []byte(policy3), 0755), check.IsNil)

	dockerCmd(c, "pull", s.reg2.URL()+"/busybox:reg2")
}
