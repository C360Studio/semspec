// Package main provides a command-line tool for generating OpenAPI specifications
// and component configuration schemas.
//
// It collects service OpenAPI specs from registered components and generates:
//   - A combined OpenAPI 3.0 specification file (specs/openapi.v3.yaml)
//   - JSON Schema files for each component configuration (schemas/*.v1.json)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	// Import all semspec component packages to register their schemas
	astindexer "github.com/c360studio/semspec/processor/ast-indexer"
	"github.com/c360studio/semspec/processor/constitution"
	questionanswerer "github.com/c360studio/semspec/processor/question-answerer"
	questiontimeout "github.com/c360studio/semspec/processor/question-timeout"
	rdfexport "github.com/c360studio/semspec/processor/rdf-export"
	workflowvalidator "github.com/c360studio/semspec/processor/workflow-validator"
	workflowdocuments "github.com/c360studio/semspec/output/workflow-documents"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/service"
	"gopkg.in/yaml.v3"
)

// componentRegistry maps component names to their config types and descriptions.
// This allows us to generate schemas for all semspec-specific components.
var componentRegistry = map[string]struct {
	ConfigType  reflect.Type
	Description string
	Domain      string
}{
	"ast-indexer": {
		ConfigType:  reflect.TypeOf(astindexer.Config{}),
		Description: "Indexes source code AST and publishes entities to the graph",
		Domain:      "semspec",
	},
	"constitution": {
		ConfigType:  reflect.TypeOf(constitution.Config{}),
		Description: "Manages project constitution rules and enforcement",
		Domain:      "semspec",
	},
	"question-answerer": {
		ConfigType:  reflect.TypeOf(questionanswerer.Config{}),
		Description: "Answers questions using LLM with knowledge graph context",
		Domain:      "semspec",
	},
	"question-timeout": {
		ConfigType:  reflect.TypeOf(questiontimeout.Config{}),
		Description: "Monitors question SLAs and triggers timeouts/escalations",
		Domain:      "semspec",
	},
	"rdf-export": {
		ConfigType:  reflect.TypeOf(rdfexport.Config{}),
		Description: "Exports graph entities to RDF formats (Turtle, N-Triples, JSON-LD)",
		Domain:      "semspec",
	},
	"workflow-validator": {
		ConfigType:  reflect.TypeOf(workflowvalidator.Config{}),
		Description: "Validates workflow documents against schemas",
		Domain:      "semspec",
	},
	"workflow-documents": {
		ConfigType:  reflect.TypeOf(workflowdocuments.Config{}),
		Description: "Writes workflow documents to the filesystem",
		Domain:      "semspec",
	},
}

func main() {
	openapiOut := flag.String("o", "./specs/openapi.v3.yaml", "Output path for OpenAPI spec")
	schemasOut := flag.String("schemas", "./schemas", "Output directory for component schemas")
	flag.Parse()

	log.Printf("Semspec OpenAPI Generator")
	log.Printf("  OpenAPI output: %s", *openapiOut)
	log.Printf("  Schemas output: %s", *schemasOut)

	// Generate component configuration schemas
	if *schemasOut != "" {
		if err := os.MkdirAll(*schemasOut, 0755); err != nil {
			log.Fatalf("Failed to create schemas directory: %v", err)
		}

		if err := generateComponentSchemas(*schemasOut); err != nil {
			log.Fatalf("Failed to generate component schemas: %v", err)
		}
	}

	// Get all registered service OpenAPI specs
	serviceSpecs := service.GetAllOpenAPISpecs()
	log.Printf("Found %d service OpenAPI specs", len(serviceSpecs))

	for name := range serviceSpecs {
		log.Printf("  - %s", name)
	}

	// Create output directory if needed
	if *openapiOut != "" {
		openapiDir := filepath.Dir(*openapiOut)
		if err := os.MkdirAll(openapiDir, 0755); err != nil {
			log.Fatalf("Failed to create output directory: %v", err)
		}

		openapi := generateOpenAPISpec(serviceSpecs)
		if err := writeYAMLFile(*openapiOut, openapi); err != nil {
			log.Fatalf("Failed to write OpenAPI spec: %v", err)
		}

		log.Printf("  ✓ Generated OpenAPI spec: %s", *openapiOut)
	}

	log.Printf("✅ Generation complete!")
}

// generateComponentSchemas generates JSON Schema files for all registered components.
func generateComponentSchemas(outDir string) error {
	// Get sorted component names for deterministic output
	var names []string
	for name := range componentRegistry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := componentRegistry[name]

		// Generate schema from struct type using semstreams' schema generator
		configSchema := component.GenerateConfigSchema(info.ConfigType)

		// Convert to JSON Schema format
		jsonSchema := convertToJSONSchema(name, info.Description, info.Domain, configSchema)

		// Write to file
		outFile := filepath.Join(outDir, fmt.Sprintf("%s.v1.json", name))
		if err := writeJSONSchema(outFile, jsonSchema); err != nil {
			return fmt.Errorf("failed to write schema for %s: %w", name, err)
		}

		log.Printf("  ✓ Generated component schema: %s", outFile)
	}

	return nil
}

// ComponentSchema represents the JSON Schema for a component configuration.
type ComponentSchema struct {
	Schema      string                    `json:"$schema"`
	ID          string                    `json:"$id"`
	Type        string                    `json:"type"`
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	Properties  map[string]PropertySchema `json:"properties"`
	Required    []string                  `json:"required"`
	Metadata    ComponentMetadata         `json:"x-component-metadata"`
}

// ComponentMetadata holds component metadata for the schema.
type ComponentMetadata struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	Version string `json:"version"`
}

// PropertySchema represents a JSON Schema property definition.
type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *int     `json:"minimum,omitempty"`
	Maximum     *int     `json:"maximum,omitempty"`
	Category    string   `json:"category,omitempty"`
}

// convertToJSONSchema converts a component.ConfigSchema to a JSON Schema.
func convertToJSONSchema(name, description, domain string, schema component.ConfigSchema) ComponentSchema {
	properties := make(map[string]PropertySchema)

	for propName, propSchema := range schema.Properties {
		properties[propName] = PropertySchema{
			Type:        mapTypeToJSONSchema(propSchema.Type),
			Description: propSchema.Description,
			Default:     propSchema.Default,
			Enum:        propSchema.Enum,
			Minimum:     propSchema.Minimum,
			Maximum:     propSchema.Maximum,
			Category:    propSchema.Category,
		}
	}

	// Ensure Required is an empty array instead of nil
	required := schema.Required
	if required == nil {
		required = []string{}
	}

	return ComponentSchema{
		Schema:      "http://json-schema.org/draft-07/schema#",
		ID:          fmt.Sprintf("%s.v1.json", name),
		Type:        "object",
		Title:       fmt.Sprintf("%s Configuration", name),
		Description: description,
		Properties:  properties,
		Required:    required,
		Metadata: ComponentMetadata{
			Name:    name,
			Type:    "processor",
			Domain:  domain,
			Version: "1.0.0",
		},
	}
}

// mapTypeToJSONSchema maps component property types to JSON Schema types.
func mapTypeToJSONSchema(propType string) string {
	switch propType {
	case "int", "integer", "float":
		return "number"
	case "bool":
		return "boolean"
	case "array":
		return "array"
	case "object", "ports", "cache":
		return "object"
	default:
		return "string"
	}
}

// writeJSONSchema writes a ComponentSchema to a JSON file.
func writeJSONSchema(filename string, schema ComponentSchema) error {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// OpenAPIDocument represents the complete OpenAPI 3.0 specification.
type OpenAPIDocument struct {
	OpenAPI    string              `yaml:"openapi"`
	Info       InfoObject          `yaml:"info"`
	Servers    []ServerObject      `yaml:"servers"`
	Paths      map[string]PathItem `yaml:"paths"`
	Components ComponentsObject    `yaml:"components"`
	Tags       []TagObject         `yaml:"tags"`
}

// InfoObject contains API metadata.
type InfoObject struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

// ServerObject defines an API server.
type ServerObject struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

// ComponentsObject holds reusable objects.
type ComponentsObject struct {
	Schemas map[string]any `yaml:"schemas"`
}

// TagObject defines an API tag.
type TagObject struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// PathItem describes operations available on a path.
type PathItem struct {
	Get    *Operation `yaml:"get,omitempty"`
	Post   *Operation `yaml:"post,omitempty"`
	Put    *Operation `yaml:"put,omitempty"`
	Delete *Operation `yaml:"delete,omitempty"`
}

// Operation describes a single API operation.
type Operation struct {
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description,omitempty"`
	Tags        []string            `yaml:"tags,omitempty"`
	Parameters  []Parameter         `yaml:"parameters,omitempty"`
	Responses   map[string]Response `yaml:"responses"`
}

// Parameter describes an operation parameter.
type Parameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"`
	Required    bool      `yaml:"required,omitempty"`
	Description string    `yaml:"description,omitempty"`
	Schema      SchemaRef `yaml:"schema"`
}

// Response describes an operation response.
type Response struct {
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content,omitempty"`
}

// MediaType describes a media type and schema.
type MediaType struct {
	Schema SchemaRef `yaml:"schema"`
}

// SchemaRef references a schema.
type SchemaRef struct {
	Ref   string     `yaml:"$ref,omitempty"`
	Type  string     `yaml:"type,omitempty"`
	Items *SchemaRef `yaml:"items,omitempty"`
}

// generateOpenAPISpec generates an OpenAPI 3.0 specification from service specs.
func generateOpenAPISpec(serviceSpecs map[string]*service.OpenAPISpec) OpenAPIDocument {
	paths := buildPathsFromRegistry(serviceSpecs)
	schemas := buildSchemasFromRegistry(serviceSpecs)
	tags := buildTagsFromRegistry(serviceSpecs)

	return OpenAPIDocument{
		OpenAPI: "3.0.3",
		Info: InfoObject{
			Title:       "Semspec API",
			Description: "HTTP API for semantic development agent - constitution management, AST indexing, and development workflow automation",
			Version:     "1.0.0",
		},
		Servers: []ServerObject{
			{URL: "http://localhost:8080", Description: "Development server"},
		},
		Paths:      paths,
		Components: ComponentsObject{Schemas: schemas},
		Tags:       tags,
	}
}

// buildPathsFromRegistry creates OpenAPI paths from the service registry.
func buildPathsFromRegistry(specs map[string]*service.OpenAPISpec) map[string]PathItem {
	paths := make(map[string]PathItem)

	var names []string
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := specs[name]
		for path, pathSpec := range spec.Paths {
			pathItem := convertPathSpec(pathSpec)
			paths[path] = pathItem
		}
	}

	return paths
}

// convertPathSpec converts service.PathSpec to local PathItem.
func convertPathSpec(ps service.PathSpec) PathItem {
	item := PathItem{}

	if ps.GET != nil {
		item.Get = convertOperation(ps.GET)
	}
	if ps.POST != nil {
		item.Post = convertOperation(ps.POST)
	}
	if ps.PUT != nil {
		item.Put = convertOperation(ps.PUT)
	}
	if ps.DELETE != nil {
		item.Delete = convertOperation(ps.DELETE)
	}

	return item
}

// convertOperation converts service.OperationSpec to local Operation.
func convertOperation(op *service.OperationSpec) *Operation {
	operation := &Operation{
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Responses:   make(map[string]Response),
	}

	for _, p := range op.Parameters {
		operation.Parameters = append(operation.Parameters, Parameter{
			Name:        p.Name,
			In:          p.In,
			Required:    p.Required,
			Description: p.Description,
			Schema:      SchemaRef{Type: p.Schema.Type},
		})
	}

	for code, resp := range op.Responses {
		response := Response{
			Description: resp.Description,
		}

		if resp.SchemaRef != "" {
			contentType := resp.ContentType
			if contentType == "" {
				contentType = "application/json"
			}

			var schema SchemaRef
			if resp.IsArray {
				schema = SchemaRef{
					Type:  "array",
					Items: &SchemaRef{Ref: resp.SchemaRef},
				}
			} else {
				schema = SchemaRef{Ref: resp.SchemaRef}
			}

			response.Content = map[string]MediaType{
				contentType: {Schema: schema},
			}
		} else if resp.ContentType != "" && resp.ContentType != "text/event-stream" {
			response.Content = map[string]MediaType{
				resp.ContentType: {
					Schema: SchemaRef{Type: "object"},
				},
			}
		}

		operation.Responses[code] = response
	}

	return operation
}

// buildTagsFromRegistry collects all unique tags from service specs.
func buildTagsFromRegistry(specs map[string]*service.OpenAPISpec) []TagObject {
	tagMap := make(map[string]TagObject)

	for _, spec := range specs {
		for _, tag := range spec.Tags {
			if _, exists := tagMap[tag.Name]; !exists {
				tagMap[tag.Name] = TagObject{
					Name:        tag.Name,
					Description: tag.Description,
				}
			}
		}
	}

	var tags []TagObject
	var names []string
	for name := range tagMap {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tags = append(tags, tagMap[name])
	}

	return tags
}

// buildSchemasFromRegistry generates JSON schemas for all response types.
func buildSchemasFromRegistry(specs map[string]*service.OpenAPISpec) map[string]any {
	schemas := make(map[string]any)
	seen := make(map[reflect.Type]bool)

	var names []string
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := specs[name]
		for _, t := range spec.ResponseTypes {
			if seen[t] {
				continue
			}
			seen[t] = true

			typeName := typeNameFromReflect(t)
			schemas[typeName] = schemaFromType(t)
		}
	}

	return schemas
}

// schemaFromType generates a JSON Schema from a reflect.Type.
func schemaFromType(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Ptr {
		schema := schemaFromType(t.Elem())
		schema["nullable"] = true
		return schema
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}

	case reflect.Bool:
		return map[string]any{"type": "boolean"}

	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		return schemaFromStruct(t)

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{
			"type":  "array",
			"items": schemaFromType(t.Elem()),
		}

	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaFromType(t.Elem()),
		}

	case reflect.Interface:
		return map[string]any{}

	default:
		return map[string]any{"type": "string"}
	}
}

// schemaFromStruct generates a JSON Schema object definition from a struct type.
func schemaFromStruct(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		name, opts := parseJSONTag(jsonTag)
		if name == "" {
			name = field.Name
		}

		fieldSchema := schemaFromType(field.Type)

		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema["description"] = desc
		}

		properties[name] = fieldSchema

		if !strings.Contains(opts, "omitempty") && field.Type.Kind() != reflect.Ptr {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// parseJSONTag parses a json struct tag and returns the name and options.
func parseJSONTag(tag string) (name string, opts string) {
	if tag == "" {
		return "", ""
	}

	parts := strings.Split(tag, ",")
	name = parts[0]

	if len(parts) > 1 {
		opts = strings.Join(parts[1:], ",")
	}

	return name, opts
}

// typeNameFromReflect extracts a clean type name from a reflect.Type.
func typeNameFromReflect(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		return typeNameFromReflect(t.Elem())
	}

	name := t.Name()
	if name == "" {
		name = t.String()
	}

	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}

	return name
}

// writeYAMLFile writes a struct to a YAML file.
func writeYAMLFile(filename string, data any) error {
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	header := []byte(strings.TrimSpace(`
# OpenAPI 3.0 Specification for Semspec API
# Generated by openapi-generator tool
# DO NOT EDIT MANUALLY - This file is auto-generated from service registrations
`) + "\n\n")

	content := append(header, yamlData...)

	if err := os.WriteFile(filename, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
