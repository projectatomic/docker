package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stringutils"
	"github.com/go-check/check"
)

func (s *DockerSuite) TestInspectApiContainerResponse(c *check.C) {
	testRequires(c, DaemonIsLinux)
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")

	cleanedContainerID := strings.TrimSpace(out)
	keysBase := []string{"Id", "State", "Created", "Path", "Args", "Config", "Image", "NetworkSettings",
		"ResolvConfPath", "HostnamePath", "HostsPath", "LogPath", "Name", "Driver", "ExecDriver", "MountLabel", "ProcessLabel", "GraphDriver"}

	cases := []struct {
		version string
		keys    []string
	}{
		{"1.20", append(keysBase, "Mounts")},
		{"1.19", append(keysBase, "Volumes", "VolumesRW")},
	}

	for _, cs := range cases {
		endpoint := fmt.Sprintf("/v%s/containers/%s/json", cs.version, cleanedContainerID)

		status, body, err := sockRequest("GET", endpoint, nil)
		c.Assert(status, check.Equals, http.StatusOK)
		c.Assert(err, check.IsNil)

		var inspectJSON map[string]interface{}
		if err = json.Unmarshal(body, &inspectJSON); err != nil {
			c.Fatalf("unable to unmarshal body for version %s: %v", cs.version, err)
		}

		for _, key := range cs.keys {
			if _, ok := inspectJSON[key]; !ok {
				c.Fatalf("%s does not exist in response for version %s", key, cs.version)
			}
		}

		//Issue #6830: type not properly converted to JSON/back
		if _, ok := inspectJSON["Path"].(bool); ok {
			c.Fatalf("Path of `true` should not be converted to boolean `true` via JSON marshalling")
		}
	}
}

func (s *DockerSuite) TestInspectApiContainerVolumeDriverLegacy(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")

	cleanedContainerID := strings.TrimSpace(out)

	cases := []string{"1.19", "1.20"}
	for _, version := range cases {
		endpoint := fmt.Sprintf("/v%s/containers/%s/json", version, cleanedContainerID)
		status, body, err := sockRequest("GET", endpoint, nil)
		c.Assert(status, check.Equals, http.StatusOK)
		c.Assert(err, check.IsNil)

		var inspectJSON map[string]interface{}
		if err = json.Unmarshal(body, &inspectJSON); err != nil {
			c.Fatalf("unable to unmarshal body for version %s: %v", version, err)
		}

		config, ok := inspectJSON["Config"]
		if !ok {
			c.Fatal("Unable to find 'Config'")
		}
		cfg := config.(map[string]interface{})
		if _, ok := cfg["VolumeDriver"]; !ok {
			c.Fatalf("Api version %s expected to include VolumeDriver in 'Config'", version)
		}
	}
}

func (s *DockerSuite) TestInspectApiContainerVolumeDriver(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")

	cleanedContainerID := strings.TrimSpace(out)

	endpoint := fmt.Sprintf("/v1.21/containers/%s/json", cleanedContainerID)
	status, body, err := sockRequest("GET", endpoint, nil)
	c.Assert(status, check.Equals, http.StatusOK)
	c.Assert(err, check.IsNil)

	var inspectJSON map[string]interface{}
	if err = json.Unmarshal(body, &inspectJSON); err != nil {
		c.Fatalf("unable to unmarshal body for version 1.21: %v", err)
	}

	config, ok := inspectJSON["Config"]
	if !ok {
		c.Fatal("Unable to find 'Config'")
	}
	cfg := config.(map[string]interface{})
	if _, ok := cfg["VolumeDriver"]; ok {
		c.Fatal("Api version 1.21 expected to not include VolumeDriver in 'Config'")
	}

	config, ok = inspectJSON["HostConfig"]
	if !ok {
		c.Fatal("Unable to find 'HostConfig'")
	}
	cfg = config.(map[string]interface{})
	if _, ok := cfg["VolumeDriver"]; !ok {
		c.Fatal("Api version 1.21 expected to include VolumeDriver in 'HostConfig'")
	}
}

func (s *DockerSuite) TestInspectApiImageResponse(c *check.C) {
	dockerCmd(c, "tag", "busybox:latest", "busybox:mytag")

	endpoint := "/images/busybox/json"
	status, body, err := sockRequest("GET", endpoint, nil)

	c.Assert(err, check.IsNil)
	c.Assert(status, check.Equals, http.StatusOK)

	var imageJSON types.ImageInspect
	if err = json.Unmarshal(body, &imageJSON); err != nil {
		c.Fatalf("unable to unmarshal body for latest version: %v", err)
	}

	c.Assert(len(imageJSON.Tags), check.Equals, 2)

	c.Assert(stringutils.InSlice(imageJSON.Tags, "busybox:latest"), check.Equals, true)
	c.Assert(stringutils.InSlice(imageJSON.Tags, "busybox:mytag"), check.Equals, true)
}

func (s *DockerRegistrySuite) TestInspectApiRemoteImage(c *check.C) {
	repoName := fmt.Sprintf("%v/dockercli/busybox", s.reg.url)
	// tag the image and upload it to the private registry
	dockerCmd(c, "tag", "busybox", repoName)
	defer deleteImages(repoName)

	dockerCmd(c, "push", repoName)
	localValue, _, _ := apiCallInspectImage(c, nil, repoName, false, false)
	remoteValue, _, _ := apiCallInspectImage(c, nil, repoName, true, false)
	compareInspectValues(c, "a", localValue, remoteValue, true)

	deleteImages(repoName)

	// local inspect shall fail now
	_, status, _ := apiCallInspectImage(c, nil, repoName, false, true)
	c.Assert(status, check.Equals, http.StatusNotFound)

	// remote inspect shall still succeed
	remoteValue2, _, _ := apiCallInspectImage(c, nil, repoName, true, false)
	compareInspectValues(c, "a", localValue, remoteValue2, true)
}

func (s *DockerRegistrySuite) TestInspectApiImageFromAdditionalRegistry(c *check.C) {
	d := NewDaemon(c)
	daemonArgs := []string{"--add-registry=" + s.reg.url}
	if err := d.StartWithBusybox(daemonArgs...); err != nil {
		c.Fatalf("we should have been able to start the daemon with passing { %s } flags: %v", strings.Join(daemonArgs, ", "), err)
	}
	defer d.Stop()

	repoName := fmt.Sprintf("dockercli/busybox")
	fqn := s.reg.url + "/" + repoName
	// tag the image and upload it to the private registry
	if out, err := d.Cmd("tag", "busybox", fqn); err != nil {
		c.Fatalf("image tagging failed: %s, %v", out, err)
	}

	localValue, _, _ := apiCallInspectImage(c, d, repoName, false, false)

	_, status, _ := apiCallInspectImage(c, d, repoName, true, true)
	c.Assert(status, check.Equals, http.StatusNotFound)

	if out, err := d.Cmd("push", fqn); err != nil {
		c.Fatalf("failed to push image %s: error %v, output %q", fqn, err, out)
	}

	remoteValue, _, _ := apiCallInspectImage(c, d, repoName, true, false)
	compareInspectValues(c, "a", localValue, remoteValue, true)

	if out, err := d.Cmd("rmi", fqn); err != nil {
		c.Fatalf("failed to remove image %s: %s, %v", fqn, out, err)
	}

	remoteValue2, _, _ := apiCallInspectImage(c, d, fqn, true, false)
	compareInspectValues(c, "a", localValue, remoteValue2, true)
}

func (s *DockerRegistrySuite) TestInspectApiNonExistentRepository(c *check.C) {
	repoName := fmt.Sprintf("%s/foo/non-existent", s.reg.url)

	_, status, err := apiCallInspectImage(c, nil, repoName, false, true)
	c.Assert(status, check.Equals, http.StatusNotFound)
	c.Assert(err, check.Not(check.IsNil))
	c.Assert(err.Error(), check.Matches, `(?i)no such image.*`)

	_, status, err = apiCallInspectImage(c, nil, repoName, true, true)
	c.Assert(err, check.Not(check.IsNil))
	c.Assert(err.Error(), check.Matches, `(?i).*(not found|no such image|no tags available).*`)
}
