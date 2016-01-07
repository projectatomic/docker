package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/versions/v1p20"
	"github.com/docker/docker/cliconfig"
	"github.com/docker/docker/pkg/integration/checker"
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
		{"v1.20", append(keysBase, "Mounts")},
		{"v1.19", append(keysBase, "Volumes", "VolumesRW")},
	}

	for _, cs := range cases {
		body := getInspectBody(c, cs.version, cleanedContainerID)

		var inspectJSON map[string]interface{}
		if err := json.Unmarshal(body, &inspectJSON); err != nil {
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

	cases := []string{"v1.19", "v1.20"}
	for _, version := range cases {
		body := getInspectBody(c, version, cleanedContainerID)

		var inspectJSON map[string]interface{}
		if err := json.Unmarshal(body, &inspectJSON); err != nil {
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

	body := getInspectBody(c, "v1.21", cleanedContainerID)

	var inspectJSON map[string]interface{}
	if err := json.Unmarshal(body, &inspectJSON); err != nil {
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

	c.Assert(len(imageJSON.RepoTags), check.Equals, 2)

	c.Assert(stringutils.InSlice(imageJSON.RepoTags, "busybox:latest"), check.Equals, true)
	c.Assert(stringutils.InSlice(imageJSON.RepoTags, "busybox:mytag"), check.Equals, true)
}

// #17131, #17139, #17173
func (s *DockerSuite) TestInspectApiEmptyFieldsInConfigPre121(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")

	cleanedContainerID := strings.TrimSpace(out)

	cases := []string{"v1.19", "v1.20"}
	for _, version := range cases {
		body := getInspectBody(c, version, cleanedContainerID)

		var inspectJSON map[string]interface{}
		if err := json.Unmarshal(body, &inspectJSON); err != nil {
			c.Fatalf("unable to unmarshal body for version %s: %v", version, err)
		}

		config, ok := inspectJSON["Config"]
		if !ok {
			c.Fatal("Unable to find 'Config'")
		}
		cfg := config.(map[string]interface{})
		for _, f := range []string{"MacAddress", "NetworkDisabled", "ExposedPorts"} {
			if _, ok := cfg[f]; !ok {
				c.Fatalf("Api version %s expected to include %s in 'Config'", version, f)
			}
		}
	}
}

func (s *DockerSuite) TestInspectApiBridgeNetworkSettings120(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")

	cleanedContainerID := strings.TrimSpace(out)
	body := getInspectBody(c, "v1.20", cleanedContainerID)

	var inspectJSON v1p20.ContainerJSON
	err := json.Unmarshal(body, &inspectJSON)
	c.Assert(err, checker.IsNil)

	settings := inspectJSON.NetworkSettings
	c.Assert(settings.IPAddress, checker.Not(checker.HasLen), 0)
}

func (s *DockerSuite) TestInspectApiBridgeNetworkSettings121(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "busybox", "true")
	cleanedContainerID := strings.TrimSpace(out)

	body := getInspectBody(c, "v1.21", cleanedContainerID)

	var inspectJSON types.ContainerJSON
	err := json.Unmarshal(body, &inspectJSON)
	c.Assert(err, checker.IsNil)

	settings := inspectJSON.NetworkSettings
	c.Assert(settings.IPAddress, checker.Not(checker.HasLen), 0)
	c.Assert(settings.Networks["bridge"], checker.Not(checker.IsNil))
	c.Assert(settings.IPAddress, checker.Equals, settings.Networks["bridge"].IPAddress)
}

func compareInspectValues(c *check.C, name string, fst, snd interface{}, localVsRemote bool) {
	additionalLocalAttributes := map[string]struct{}{}
	additionalRemoteAttributes := map[string]struct{}{}
	if localVsRemote {
		additionalLocalAttributes = map[string]struct{}{
			"GraphDriver": {},
			"VirtualSize": {},
		}
		additionalRemoteAttributes = map[string]struct{}{"Registry": {}}
	}

	isRootObject := len(name) <= 1

	compareArrays := func(lVal, rVal []interface{}) {
		if len(lVal) != len(rVal) {
			c.Errorf("array length differs between fst and snd for %q: %d != %d", name, len(lVal), len(rVal))
		}
		for i := 0; i < len(lVal) && i < len(rVal); i++ {
			compareInspectValues(c, fmt.Sprintf("%s[%d]", name, i), lVal[i], rVal[i], localVsRemote)
		}
	}

	if reflect.TypeOf(fst) != reflect.TypeOf(snd) {
		c.Errorf("types don't match for %q: %T != %T", name, fst, snd)
		return
	}
	switch fst.(type) {
	case bool:
		lVal := fst.(bool)
		rVal := snd.(bool)
		if lVal != rVal {
			c.Errorf("fst value differs from snd for %q: %t != %t", name, lVal, rVal)
		}

	case float64:
		lVal := fst.(float64)
		rVal := snd.(float64)
		if lVal != rVal {
			c.Errorf("fst value differs from snd for %q: %f != %f", name, lVal, rVal)
		}

	case string:
		lVal := fst.(string)
		rVal := snd.(string)
		if lVal != rVal {
			c.Errorf("fst value differs from snd for %q: %q != %q", name, lVal, rVal)
		}

	// JSON array
	case []interface{}:
		lVal := fst.([]interface{})
		rVal := snd.([]interface{})
		if strings.HasSuffix(name, ".RepoTags") {
			if len(rVal) != 1 {
				c.Errorf("expected one item in remote Tags, not: %d", len(rVal))
			} else {
				found := false
				for _, v := range lVal {
					if v.(string) == rVal[0].(string) {
						found = true
						break
					}
				}
				if !found {
					c.Errorf("expected remote tag %q to be in among local ones: %v", rVal[0].(string), lVal)
				}
			}
		} else if strings.HasSuffix(name, ".RepoDigests") {
			if len(lVal) >= 1 {
				compareArrays(lVal, rVal)
			}
			if len(rVal) != 1 {
				c.Errorf("expected just one item in %q array, not %d (%v)", name, len(rVal), rVal)
			}
		} else {
			compareArrays(lVal, rVal)
		}

	// JSON object
	case map[string]interface{}:
		lMap := fst.(map[string]interface{})
		rMap := snd.(map[string]interface{})
		if isRootObject && len(lMap)-len(additionalLocalAttributes) != len(rMap)-len(additionalRemoteAttributes) {
			c.Errorf("got unexpected number of root object's attributes from snd inpect %q: %d != %d", name, len(lMap)-len(additionalLocalAttributes), len(rMap)-len(additionalRemoteAttributes))
		} else if !isRootObject && len(lMap) != len(rMap) {
			c.Errorf("map length differs between fst and snd for %q: %d != %d", name, len(lMap), len(rMap))
		}
		for key, lVal := range lMap {
			itemName := fmt.Sprintf("%s.%s", name, key)
			rVal, ok := rMap[key]
			if ok {
				compareInspectValues(c, itemName, lVal, rVal, localVsRemote)
			} else if _, exists := additionalLocalAttributes[key]; !isRootObject || !localVsRemote || !exists {
				c.Errorf("attribute %q present in fst but not in snd object", itemName)
			}
		}
		for key := range rMap {
			if _, ok := lMap[key]; !ok {
				if _, exists := additionalRemoteAttributes[key]; !isRootObject || !localVsRemote || !exists {
					c.Errorf("attribute \"%s.%s\" present in snd but not in fst object", name, key)
				}
			}
		}

	case nil:
		if fst != snd {
			c.Errorf("fst value differs from snd for %q: %v (%T) != %v (%T)", name, fst, fst, snd, snd)
		}

	default:
		c.Fatalf("got unexpected type (%T) for %q", fst, name)
	}
}

func apiCallInspectImage(c *check.C, d *Daemon, repoName string, remote, shouldFail bool) (value interface{}, status int, err error) {
	suffix := ""
	if remote {
		suffix = "?remote=1"
	}
	endpoint := fmt.Sprintf("/v1.20/images/%s/json%s", repoName, suffix)
	status, body, err := func() (int, []byte, error) {
		if d == nil {
			return sockRequest("GET", endpoint, nil)
		}
		return d.sockRequest("GET", endpoint, nil)
	}()
	if shouldFail {
		c.Assert(status, check.Not(check.Equals), http.StatusOK)
		if err == nil {
			err = fmt.Errorf("%s", bytes.TrimSpace(body))
		}
	} else {
		c.Assert(status, check.Equals, http.StatusOK)
		c.Assert(err, check.IsNil)
		if err = json.Unmarshal(body, &value); err != nil {
			what := "local"
			if remote {
				what = "remote"
			}
			c.Fatalf("failed to parse result for %s image %q: %v", what, repoName, err)
		}
	}
	return
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
	daemonArgs := []string{"--add-registry=" + s.reg.url}
	if err := s.d.StartWithBusybox(daemonArgs...); err != nil {
		c.Fatalf("we should have been able to start the daemon with passing { %s } flags: %v", strings.Join(daemonArgs, ", "), err)
	}

	repoName := fmt.Sprintf("dockercli/busybox")
	fqn := s.reg.url + "/" + repoName
	// tag the image and upload it to the private registry
	if out, err := s.d.Cmd("tag", "busybox", fqn); err != nil {
		c.Fatalf("image tagging failed: %s, %v", out, err)
	}

	localValue, _, _ := apiCallInspectImage(c, s.d, repoName, false, false)

	_, status, _ := apiCallInspectImage(c, s.d, repoName, true, true)
	c.Assert(status, check.Equals, http.StatusNotFound)

	if out, err := s.d.Cmd("push", fqn); err != nil {
		c.Fatalf("failed to push image %s: error %v, output %q", fqn, err, out)
	}

	remoteValue, _, _ := apiCallInspectImage(c, s.d, repoName, true, false)
	compareInspectValues(c, "a", localValue, remoteValue, true)

	if out, err := s.d.Cmd("rmi", fqn); err != nil {
		c.Fatalf("failed to remove image %s: %s, %v", fqn, out, err)
	}

	remoteValue2, _, _ := apiCallInspectImage(c, s.d, fqn, true, false)
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
	c.Assert(err.Error(), check.Matches, `(?i).*(not found|no such image|no tags available|not known).*`)
}

func (s *DockerRegistriesSuite) TestInspectApiRemoteImageFromAdditionalRegistryWithAuth(c *check.C) {
	c.Assert(s.d.StartWithBusybox("--add-registry="+s.regWithAuth.url), check.IsNil)

	repo := fmt.Sprintf("%s/busybox", s.regWithAuth.url)
	repo2 := fmt.Sprintf("%s/runcom/busybox", s.regWithAuth.url)
	repoUnqualified := "busybox"
	repo2Unqualified := "runcom/busybox"

	out, err := s.d.Cmd("tag", "busybox", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))
	out, err = s.d.Cmd("tag", "busybox", repo2)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("login", "-u", s.regWithAuth.username, "-p", s.regWithAuth.password, "-e", s.regWithAuth.email, s.regWithAuth.url)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("push", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))
	out, err = s.d.Cmd("push", repo2)
	c.Assert(err, check.IsNil, check.Commentf(out))

	out, err = s.d.Cmd("rmi", "-f", repo)
	c.Assert(err, check.IsNil, check.Commentf(out))
	out, err = s.d.Cmd("rmi", "-f", repo2)
	c.Assert(err, check.IsNil, check.Commentf(out))

	// verify I cannot search for repo2 without authconfig - I get not found
	resp, body, err := s.d.sockRequestRaw("GET", fmt.Sprintf("/v1.21/images/%s/json?remote=1", repo2), nil, "application/json", nil)
	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusNotFound)
	content, err := readBody(body)
	c.Assert(err, check.IsNil)
	expected := fmt.Sprintf("image %s not found", repo2Unqualified)
	if !strings.Contains(string(content), expected) {
		c.Fatalf("Wanted %s, got %s", expected, string(content))
	}

	ac := cliconfig.AuthConfig{
		Username: s.regWithAuth.username,
		Password: s.regWithAuth.password,
		Email:    s.regWithAuth.email,
	}
	b, err := json.Marshal(ac)
	c.Assert(err, check.IsNil)
	authConfig := base64.URLEncoding.EncodeToString(b)

	// verify I can search for repo2 with authconfig
	resp, body, err = s.d.sockRequestRaw("GET", fmt.Sprintf("/v1.21/images/%s/json?remote=1", repo2), nil, "application/json", map[string]string{"X-Registry-Auth": authConfig})
	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusOK)
	content, err = readBody(body)
	c.Assert(err, check.IsNil)
	expected = fmt.Sprintf(`"Registry":"%s"`, s.regWithAuth.url)
	if !strings.Contains(string(content), expected) {
		c.Fatalf("Wanted %s, got %s", expected, string(content))
	}

	// verify I can search for repoUnqualified and get result from regwithauth/repoUnqualified
	resp, body, err = s.d.sockRequestRaw("GET", fmt.Sprintf("/v1.21/images/%s/json?remote=1", repoUnqualified), nil, "application/json", map[string]string{"X-Registry-Auth": authConfig})
	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusOK)
	content, err = readBody(body)
	c.Assert(err, check.IsNil)
	expected = fmt.Sprintf(`"Registry":"%s"`, s.regWithAuth.url)
	if !strings.Contains(string(content), expected) {
		c.Fatalf("Wanted %s, got %s", expected, string(content))
	}

	acs := make(map[string]cliconfig.AuthConfig, 1)
	acs[s.regWithAuth.url] = ac
	b, err = json.Marshal(ac)
	c.Assert(err, check.IsNil)
	authConfigs := base64.URLEncoding.EncodeToString(b)

	// test api call with "multiple" X-Registry-Auth still works for unqualified image
	resp, body, err = s.d.sockRequestRaw("GET", fmt.Sprintf("/v1.21/images/%s/json?remote=1", repoUnqualified), nil, "application/json", map[string]string{"X-Registry-Auth": authConfigs})
	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusOK)
	content, err = readBody(body)
	c.Assert(err, check.IsNil)
	expected = fmt.Sprintf(`"Registry":"%s"`, s.regWithAuth.url)
	if !strings.Contains(string(content), expected) {
		c.Fatalf("Wanted %s, got %s", expected, string(content))
	}
}
