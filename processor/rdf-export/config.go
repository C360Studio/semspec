package rdfexport

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/c360studio/semspec/export"
	"github.com/c360studio/semstreams/component"
	ssexport "github.com/c360studio/semstreams/vocabulary/export"
)

// rdfExportSchema defines the configuration schema.
var rdfExportSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the rdf-export output component.
type Config struct {
	Ports   *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	Format  string                `json:"format" schema:"type:string,description:RDF serialization format (turtle/ntriples/jsonld),category:basic,default:turtle"`
	Profile string                `json:"profile" schema:"type:string,description:Ontology profile (minimal/bfo/cco),category:basic,default:minimal"`
	BaseIRI string                `json:"base_iri" schema:"type:string,description:Base IRI for entity URIs,category:basic,default:https://semspec.dev"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Format != "" {
		switch strings.ToLower(c.Format) {
		case "turtle", "ntriples", "jsonld":
			// valid
		default:
			return fmt.Errorf("unsupported format: %s (valid: turtle, ntriples, jsonld)", c.Format)
		}
	}

	if c.Profile != "" {
		switch export.Profile(strings.ToLower(c.Profile)) {
		case export.ProfileMinimal, export.ProfileBFO, export.ProfileCCO:
			// valid
		default:
			return fmt.Errorf("unsupported profile: %s (valid: minimal, bfo, cco)", c.Profile)
		}
	}

	return nil
}

// GetFormat returns the configured ssexport.Format.
func (c *Config) GetFormat() ssexport.Format {
	switch strings.ToLower(c.Format) {
	case "ntriples":
		return ssexport.NTriples
	case "jsonld":
		return ssexport.JSONLD
	default:
		return ssexport.Turtle
	}
}

// GetProfile returns the configured export.Profile.
func (c *Config) GetProfile() export.Profile {
	p := export.Profile(strings.ToLower(c.Profile))
	switch p {
	case export.ProfileBFO, export.ProfileCCO:
		return p
	default:
		return export.ProfileMinimal
	}
}

// GetBaseIRI returns the configured base IRI with a default fallback.
func (c *Config) GetBaseIRI() string {
	if c.BaseIRI != "" {
		return c.BaseIRI
	}
	return "https://semspec.dev"
}

// DefaultConfig returns the default configuration for rdf-export.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "entities_in",
					Type:        "jetstream",
					Subject:     "graph.ingest.entity",
					StreamName:  "GRAPH",
					Required:    true,
					Description: "Entity ingest messages from the graph pipeline",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "rdf_out",
					Type:        "jetstream",
					Subject:     "graph.export.rdf",
					Required:    true,
					Description: "Serialized RDF output for downstream consumers",
				},
			},
		},
		Format:  "turtle",
		Profile: "minimal",
		BaseIRI: "https://semspec.dev",
	}
}
