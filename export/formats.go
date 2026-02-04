package export

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FormatInfo provides metadata about an export format.
type FormatInfo struct {
	// Name is the format identifier.
	Name Format

	// MIMEType is the standard MIME type.
	MIMEType string

	// Extension is the file extension (with dot).
	Extension string

	// Description describes the format.
	Description string
}

// FormatRegistry contains metadata for all supported formats.
var FormatRegistry = map[Format]FormatInfo{
	FormatTurtle: {
		Name:        FormatTurtle,
		MIMEType:    "text/turtle",
		Extension:   ".ttl",
		Description: "Turtle - Terse RDF Triple Language",
	},
	FormatNTriples: {
		Name:        FormatNTriples,
		MIMEType:    "application/n-triples",
		Extension:   ".nt",
		Description: "N-Triples - Line-based RDF format",
	},
	FormatJSONLD: {
		Name:        FormatJSONLD,
		MIMEType:    "application/ld+json",
		Extension:   ".jsonld",
		Description: "JSON-LD - JSON for Linked Data",
	},
}

// GetFormatInfo returns metadata for a format.
func GetFormatInfo(format Format) (FormatInfo, bool) {
	info, ok := FormatRegistry[format]
	return info, ok
}

// TurtleWriter writes RDF in Turtle format.
type TurtleWriter struct {
	prefixes map[string]string
	sb       strings.Builder
}

// NewTurtleWriter creates a new Turtle writer with default prefixes.
func NewTurtleWriter() *TurtleWriter {
	return &TurtleWriter{
		prefixes: defaultPrefixes(),
	}
}

// SetPrefix sets a namespace prefix.
func (w *TurtleWriter) SetPrefix(prefix, iri string) {
	w.prefixes[prefix] = iri
}

// WritePrefixes writes prefix declarations.
func (w *TurtleWriter) WritePrefixes() {
	// Sort prefixes for consistent output
	keys := make([]string, 0, len(w.prefixes))
	for k := range w.prefixes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, prefix := range keys {
		w.sb.WriteString(fmt.Sprintf("@prefix %s: <%s> .\n", prefix, w.prefixes[prefix]))
	}
	w.sb.WriteString("\n")
}

// WriteSubject starts a new subject block.
func (w *TurtleWriter) WriteSubject(iri string) {
	w.sb.WriteString(fmt.Sprintf("<%s>\n", iri))
}

// WriteType writes a type assertion.
func (w *TurtleWriter) WriteType(typeIRI string, last bool) {
	terminator := " ;"
	if last {
		terminator = " ."
	}
	w.sb.WriteString(fmt.Sprintf("    a <%s>%s\n", typeIRI, terminator))
}

// WritePredicate writes a predicate-object pair.
func (w *TurtleWriter) WritePredicate(predicateIRI string, object any, last bool) {
	terminator := " ;"
	if last {
		terminator = " ."
	}
	objectStr := formatObject(object)
	w.sb.WriteString(fmt.Sprintf("    <%s> %s%s\n", predicateIRI, objectStr, terminator))
}

// WriteBlank writes a blank line for readability.
func (w *TurtleWriter) WriteBlank() {
	w.sb.WriteString("\n")
}

// String returns the accumulated Turtle output.
func (w *TurtleWriter) String() string {
	return w.sb.String()
}

// NTriplesWriter writes RDF in N-Triples format.
type NTriplesWriter struct {
	sb strings.Builder
}

// NewNTriplesWriter creates a new N-Triples writer.
func NewNTriplesWriter() *NTriplesWriter {
	return &NTriplesWriter{}
}

// WriteTriple writes a single triple.
func (w *NTriplesWriter) WriteTriple(subject, predicate string, object any) {
	objectStr := formatObjectNTriples(object)
	w.sb.WriteString(fmt.Sprintf("<%s> <%s> %s .\n", subject, predicate, objectStr))
}

// WriteTypeTriple writes a type assertion triple.
func (w *NTriplesWriter) WriteTypeTriple(subject, typeIRI string) {
	rdfType := "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	w.sb.WriteString(fmt.Sprintf("<%s> <%s> <%s> .\n", subject, rdfType, typeIRI))
}

// String returns the accumulated N-Triples output.
func (w *NTriplesWriter) String() string {
	return w.sb.String()
}

// JSONLDDocument represents a JSON-LD document structure.
type JSONLDDocument struct {
	Context map[string]any `json:"@context"`
	Graph   []JSONLDNode   `json:"@graph"`
}

// JSONLDNode represents a node in a JSON-LD graph.
type JSONLDNode struct {
	ID         string         `json:"@id"`
	Type       []string       `json:"@type,omitempty"`
	Properties map[string]any `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for JSONLDNode.
func (n JSONLDNode) MarshalJSON() ([]byte, error) {
	// Create a map with all fields
	m := make(map[string]any)
	m["@id"] = n.ID
	if len(n.Type) > 0 {
		m["@type"] = n.Type
	}
	for k, v := range n.Properties {
		m[k] = v
	}
	return json.Marshal(m)
}

// JSONLDWriter writes RDF in JSON-LD format.
type JSONLDWriter struct {
	doc JSONLDDocument
}

// NewJSONLDWriter creates a new JSON-LD writer.
func NewJSONLDWriter() *JSONLDWriter {
	return &JSONLDWriter{
		doc: JSONLDDocument{
			Context: make(map[string]any),
			Graph:   make([]JSONLDNode, 0),
		},
	}
}

// SetContext sets the @context with prefixes.
func (w *JSONLDWriter) SetContext(prefixes map[string]string) {
	for k, v := range prefixes {
		w.doc.Context[k] = v
	}
}

// AddNode adds a node to the graph.
func (w *JSONLDWriter) AddNode(id string, types []string, properties map[string]any) {
	node := JSONLDNode{
		ID:         id,
		Type:       types,
		Properties: properties,
	}
	w.doc.Graph = append(w.doc.Graph, node)
}

// String returns the JSON-LD output.
func (w *JSONLDWriter) String() string {
	data, err := json.MarshalIndent(w.doc, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

// CompactJSONLD compacts a JSON-LD document using a context.
func CompactJSONLD(doc *JSONLDDocument) string {
	// For a full implementation, this would use a JSON-LD library
	// For now, we just marshal as-is
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ExpandJSONLD expands a JSON-LD document.
func ExpandJSONLD(jsonStr string) (*JSONLDDocument, error) {
	// For a full implementation, this would use a JSON-LD library
	// For now, we just unmarshal
	var doc JSONLDDocument
	if err := json.Unmarshal([]byte(jsonStr), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}
