// +build !windows

package dockerhooks

import (
	"io/ioutil"
	"os"
	"path"

	"github.com/opencontainers/specs/specs-go"
)

const hookDirPath = "/usr/libexec/oci/hooks.d"

// Generate docker hooks Prestart and poststart specs
func Generate(specHooks specs.Hooks, configPath string) specs.Hooks {
	hooks, err := getHooks()
	if err != nil {
		return specHooks
	}
	for _, item := range hooks {
		if item.Mode().IsRegular() {
			hook := path.Join(hookDirPath, item.Name())

			specHooks.Prestart = append(specHooks.Prestart, specs.Hook{
				Path: hook,
				Args: []string{hook, "prestart", configPath},
				Env:  []string{"container=docker"},
			},
			)
			specHooks.Poststop = append(specHooks.Poststop, specs.Hook{
				Path: hook,
				Args: []string{hook, "poststop", configPath},
				Env:  []string{"container=docker"},
			},
			)
		}
	}
	return specHooks
}

// TotalHooks returns the number of hooks to be used
func TotalHooks() int {
	hooks, _ := getHooks()
	return len(hooks)
}

func getHooks() ([]os.FileInfo, error) {
	// find any hooks executables
	if _, err := os.Stat(hookDirPath); os.IsNotExist(err) {
		return nil, nil
	}
	hooks, err := ioutil.ReadDir(hookDirPath)
	return hooks, err
}
