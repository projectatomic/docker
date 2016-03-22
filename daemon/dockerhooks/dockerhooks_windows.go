package dockerhooks

import "github.com/opencontainers/specs/specs-go"

// Generate docker hooks Prestart and poststart specs
func Generate(specHooks specs.Hooks, configPath string) specs.Hooks {
	return specHooks
}

// TotalHooks returns the number of hooks to be used
func TotalHooks() int {
	return 0
}
