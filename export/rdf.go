// Package export provides RDF export capabilities with BFO/CCO/PROV-O alignment.
package export

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
)

// Profile determines which ontology type assertions are included in the export.
type Profile string

const (
	// ProfileMinimal includes only PROV-O, Dublin Core, and SKOS predicates.
	ProfileMinimal Profile = "minimal"

	// ProfileBFO includes BFO type assertions plus minimal profile.
	ProfileBFO Profile = "bfo"

	// ProfileCCO includes CCO type assertions plus BFO profile.
	ProfileCCO Profile = "cco"
)

// Format specifies the output serialization format.
type Format string

const (
	// FormatTurtle produces Turtle (.ttl) output.
	FormatTurtle Format = "turtle"

	// FormatNTriples produces N-Triples (.nt) output.
	FormatNTriples Format = "ntriples"

	// FormatJSONLD produces JSON-LD (.jsonld) output.
	FormatJSONLD Format = "jsonld"
)

// Triple represents a semantic triple for export.
type Triple struct {
	Subject   string
	Predicate string
	Object    any
}

// Entity represents an exportable entity with its type and triples.
type Entity struct {
	ID         string
	EntityType semspec.EntityType
	Triples    []Triple
}

// RDFExporter exports entities to RDF with configurable ontology profiles.
type RDFExporter struct {
	profile  Profile
	entities []Entity
	prefixes map[string]string
}

// NewRDFExporter creates a new RDF exporter with the specified profile.
func NewRDFExporter(profile Profile) *RDFExporter {
	return &RDFExporter{
		profile:  profile,
		entities: make([]Entity, 0),
		prefixes: defaultPrefixes(),
	}
}

// defaultPrefixes returns the standard namespace prefixes for RDF export.
func defaultPrefixes() map[string]string {
	return map[string]string{
		"rdf":     "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
		"rdfs":    "http://www.w3.org/2000/01/rdf-schema#",
		"owl":     "http://www.w3.org/2002/07/owl#",
		"xsd":     "http://www.w3.org/2001/XMLSchema#",
		"dc":      "http://purl.org/dc/terms/",
		"skos":    "http://www.w3.org/2004/02/skos/core#",
		"prov":    "http://www.w3.org/ns/prov#",
		"bfo":     "http://purl.obolibrary.org/obo/",
		"cco":     "http://www.ontologyrepository.com/CommonCoreOntologies/",
		"semspec": semspec.Namespace,
		"entity":  semspec.EntityNamespace,
	}
}

// AddEntity adds an entity to be exported.
func (e *RDFExporter) AddEntity(entity Entity) {
	e.entities = append(e.entities, entity)
}

// AddEntityFromTriples creates and adds an entity from raw triples.
func (e *RDFExporter) AddEntityFromTriples(id string, entityType semspec.EntityType, triples []Triple) {
	e.entities = append(e.entities, Entity{
		ID:         id,
		EntityType: entityType,
		Triples:    triples,
	})
}

// Export serializes all entities to the specified format.
func (e *RDFExporter) Export(format Format) (string, error) {
	switch format {
	case FormatTurtle:
		return e.toTurtle(), nil
	case FormatNTriples:
		return e.toNTriples(), nil
	case FormatJSONLD:
		return e.toJSONLD(), nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

// toTurtle serializes to Turtle format.
func (e *RDFExporter) toTurtle() string {
	var sb strings.Builder

	// Write prefixes
	for prefix, iri := range e.prefixes {
		sb.WriteString(fmt.Sprintf("@prefix %s: <%s> .\n", prefix, iri))
	}
	sb.WriteString("\n")

	// Write each entity
	for _, entity := range e.entities {
		e.writeEntityTurtle(&sb, entity)
		sb.WriteString("\n")
	}

	return sb.String()
}

// writeEntityTurtle writes a single entity in Turtle format.
func (e *RDFExporter) writeEntityTurtle(sb *strings.Builder, entity Entity) {
	iri := entityIDToIRI(entity.ID)

	// Write subject
	sb.WriteString(fmt.Sprintf("<%s>\n", iri))

	// Write type assertions
	types := semspec.GetTypesForEntity(entity.EntityType, string(e.profile))
	for i, typeIRI := range types {
		sb.WriteString(fmt.Sprintf("    a <%s>", typeIRI))
		if i < len(types)-1 || len(entity.Triples) > 0 {
			sb.WriteString(" ;\n")
		} else {
			sb.WriteString(" .\n")
		}
	}

	// Write predicates
	for i, triple := range entity.Triples {
		predicateIRI := semspec.GetPredicateIRI(triple.Predicate)
		objectStr := formatObject(triple.Object)
		sb.WriteString(fmt.Sprintf("    <%s> %s", predicateIRI, objectStr))
		if i < len(entity.Triples)-1 {
			sb.WriteString(" ;\n")
		} else {
			sb.WriteString(" .\n")
		}
	}
}

// toNTriples serializes to N-Triples format.
func (e *RDFExporter) toNTriples() string {
	var sb strings.Builder

	rdfType := "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"

	for _, entity := range e.entities {
		iri := entityIDToIRI(entity.ID)

		// Write type assertions
		types := semspec.GetTypesForEntity(entity.EntityType, string(e.profile))
		for _, typeIRI := range types {
			sb.WriteString(fmt.Sprintf("<%s> <%s> <%s> .\n", iri, rdfType, typeIRI))
		}

		// Write predicates
		for _, triple := range entity.Triples {
			predicateIRI := semspec.GetPredicateIRI(triple.Predicate)
			objectStr := formatObjectNTriples(triple.Object)
			sb.WriteString(fmt.Sprintf("<%s> <%s> %s .\n", iri, predicateIRI, objectStr))
		}
	}

	return sb.String()
}

// toJSONLD serializes to JSON-LD format.
func (e *RDFExporter) toJSONLD() string {
	var sb strings.Builder

	sb.WriteString("{\n")
	sb.WriteString("  \"@context\": {\n")

	// Write context prefixes
	prefixKeys := make([]string, 0, len(e.prefixes))
	for k := range e.prefixes {
		prefixKeys = append(prefixKeys, k)
	}
	for i, prefix := range prefixKeys {
		sb.WriteString(fmt.Sprintf("    \"%s\": \"%s\"", prefix, e.prefixes[prefix]))
		if i < len(prefixKeys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("  },\n")
	sb.WriteString("  \"@graph\": [\n")

	// Write each entity
	for i, entity := range e.entities {
		e.writeEntityJSONLD(&sb, entity)
		if i < len(e.entities)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("  ]\n")
	sb.WriteString("}\n")

	return sb.String()
}

// writeEntityJSONLD writes a single entity in JSON-LD format.
func (e *RDFExporter) writeEntityJSONLD(sb *strings.Builder, entity Entity) {
	iri := entityIDToIRI(entity.ID)
	types := semspec.GetTypesForEntity(entity.EntityType, string(e.profile))

	sb.WriteString("    {\n")
	sb.WriteString(fmt.Sprintf("      \"@id\": \"%s\",\n", iri))

	// Write types
	sb.WriteString("      \"@type\": [")
	for i, t := range types {
		sb.WriteString(fmt.Sprintf("\"%s\"", t))
		if i < len(types)-1 {
			sb.WriteString(", ")
		}
	}
	sb.WriteString("]")

	// Write predicates
	for _, triple := range entity.Triples {
		sb.WriteString(",\n")
		predicateIRI := semspec.GetPredicateIRI(triple.Predicate)
		objectVal := formatObjectJSONLD(triple.Object)
		sb.WriteString(fmt.Sprintf("      \"%s\": %s", predicateIRI, objectVal))
	}

	sb.WriteString("\n    }")
}

// entityIDToIRI converts a dotted entity ID to an IRI.
// Example: "acme.semspec.project.proposal.api.auth-refresh"
//       -> "https://semspec.dev/entity/proposal/api/auth-refresh"
func entityIDToIRI(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) < 6 {
		// Not enough parts, use as-is
		return semspec.EntityNamespace + entityID
	}

	// Extract meaningful parts: skip org (0), "semspec" (1), context (2)
	// Use domain (3), type (4), instance (5+)
	domain := parts[3]
	entityType := parts[4]
	instance := strings.Join(parts[5:], "/")

	return fmt.Sprintf("%s%s/%s/%s", semspec.EntityNamespace, domain, entityType, instance)
}

// formatObject formats an object value for Turtle output.
func formatObject(obj any) string {
	switch v := obj.(type) {
	case string:
		// Check if it looks like an entity reference or IRI
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return fmt.Sprintf("<%s>", v)
		}
		if strings.Contains(v, ".") && !strings.Contains(v, " ") && len(strings.Split(v, ".")) >= 4 {
			// Likely an entity ID
			return fmt.Sprintf("<%s>", entityIDToIRI(v))
		}
		// Check for datetime
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return fmt.Sprintf("\"%s\"^^xsd:dateTime", v)
		}
		return fmt.Sprintf("\"%s\"", escapeString(v))
	case int, int32, int64:
		return fmt.Sprintf("\"%d\"^^xsd:integer", v)
	case float32, float64:
		return fmt.Sprintf("\"%f\"^^xsd:decimal", v)
	case bool:
		return fmt.Sprintf("\"%t\"^^xsd:boolean", v)
	default:
		return fmt.Sprintf("\"%v\"", v)
	}
}

// formatObjectNTriples formats an object value for N-Triples output.
func formatObjectNTriples(obj any) string {
	switch v := obj.(type) {
	case string:
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return fmt.Sprintf("<%s>", v)
		}
		if strings.Contains(v, ".") && !strings.Contains(v, " ") && len(strings.Split(v, ".")) >= 4 {
			return fmt.Sprintf("<%s>", entityIDToIRI(v))
		}
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return fmt.Sprintf("\"%s\"^^<http://www.w3.org/2001/XMLSchema#dateTime>", v)
		}
		return fmt.Sprintf("\"%s\"", escapeString(v))
	case int, int32, int64:
		return fmt.Sprintf("\"%d\"^^<http://www.w3.org/2001/XMLSchema#integer>", v)
	case float32, float64:
		return fmt.Sprintf("\"%f\"^^<http://www.w3.org/2001/XMLSchema#decimal>", v)
	case bool:
		return fmt.Sprintf("\"%t\"^^<http://www.w3.org/2001/XMLSchema#boolean>", v)
	default:
		return fmt.Sprintf("\"%v\"", v)
	}
}

// formatObjectJSONLD formats an object value for JSON-LD output.
func formatObjectJSONLD(obj any) string {
	switch v := obj.(type) {
	case string:
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return fmt.Sprintf("{\"@id\": \"%s\"}", v)
		}
		if strings.Contains(v, ".") && !strings.Contains(v, " ") && len(strings.Split(v, ".")) >= 4 {
			return fmt.Sprintf("{\"@id\": \"%s\"}", entityIDToIRI(v))
		}
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return fmt.Sprintf("{\"@value\": \"%s\", \"@type\": \"xsd:dateTime\"}", v)
		}
		return fmt.Sprintf("\"%s\"", escapeString(v))
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%f", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return fmt.Sprintf("\"%v\"", v)
	}
}

// escapeString escapes special characters in strings for RDF serialization.
func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
