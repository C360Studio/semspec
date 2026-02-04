// Package main provides a command-line tool for generating OpenAPI specifications.
// It collects service OpenAPI specs from registered components and generates
// a combined OpenAPI 3.0 specification file.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	// Import constitution package to trigger init() registration of OpenAPI spec
	_ "github.com/c360studio/semspec/processor/constitution"

	"github.com/c360studio/semstreams/service"
	"gopkg.in/yaml.v3"
)

func main() {
	openapiOut := flag.String("o", "./specs/openapi.v3.yaml", "Output path for OpenAPI spec")
	flag.Parse()

	log.Printf("Semspec OpenAPI Generator")
	log.Printf("  Output: %s", *openapiOut)

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

		log.Printf("Generated OpenAPI spec: %s", *openapiOut)
	}

	log.Printf("OpenAPI generation complete!")
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
