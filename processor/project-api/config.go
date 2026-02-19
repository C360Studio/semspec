package projectapi

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// projectAPISchema holds the configuration schema generated from Config.
var projectAPISchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the project-api component.
type Config struct {
	// RepoPath is the repository root directory to inspect.
	// When empty the component falls back to the SEMSPEC_REPO_PATH environment
	// variable, then to the process working directory.
	RepoPath string `json:"repo_path" schema:"type:string,description:Repository root path,category:basic,default:"`

	// Ports declares optional HTTP port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{}
}

// Validate verifies the configuration is consistent.
// RepoPath is optional — the component resolves it at runtime.
func (c *Config) Validate() error {
	return nil
}
