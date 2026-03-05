// Package create implements the create_tool tool executor.
// It is an MVP passthrough tool — the LLM provides a FlowSpec in its tool
// call arguments, the executor validates it and echoes it back as JSON.
// No actual tool execution or registration happens in this version.
package create

import (
	"fmt"
	"regexp"
)

// maxProcessors caps the number of processors accepted in a single FlowSpec
// to prevent resource exhaustion from malformed or adversarial LLM responses.
const maxProcessors = 20

// validNameRe matches names that are alphanumeric plus underscores, up to 64 chars.
// This mirrors the naming constraints used by agentic-tools for tool registration.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

// FlowSpec defines a dynamically created tool. The LLM provides this structure
// in the create_tool call arguments; the executor validates and echoes it back.
// In a future phase this spec would be registered and executed by the agent tree.
type FlowSpec struct {
	// Name is the tool's unique identifier. Must be alphanumeric plus underscores,
	// max 64 characters. Used as the tool name in agentic-tools registration.
	Name string `json:"name"`

	// Description is a human-readable explanation of what the tool does.
	// Exposed to the LLM in tool listings.
	Description string `json:"description"`

	// Parameters is a JSON Schema object describing the tool's input shape.
	// Passed verbatim to the agentic-tools tool definition.
	Parameters map[string]any `json:"parameters"`

	// Processors is an ordered list of component references to invoke.
	// At least one processor is required.
	Processors []ProcessorRef `json:"processors"`

	// Wiring describes how data flows between processors and the tool's
	// input/output boundaries.
	Wiring []WiringRule `json:"wiring"`
}

// ProcessorRef is a reference to a registered semstreams component that
// the flow will invoke as a step.
type ProcessorRef struct {
	// ID is a unique step identifier within this flow. Referenced by WiringRules.
	ID string `json:"id"`

	// Component is the component type name (e.g., "llm-agent", "validator").
	// Must match a registered semstreams component type.
	Component string `json:"component"`

	// Config holds step-specific configuration overrides applied on top of
	// the component's base configuration.
	Config map[string]any `json:"config"`
}

// WiringRule routes data from one part of the flow to another.
// Sources and destinations use dot-separated paths:
//   - "input.field"           — top-level tool input field
//   - "step-id.output.field" — output field of a processor step
//   - "output.field"          — tool output field
type WiringRule struct {
	// From is the data source path. Valid prefixes: "input.", "<step-id>.output.".
	From string `json:"from"`

	// To is the data destination path. Valid prefixes: "<step-id>.input.", "output.".
	To string `json:"to"`
}

// Validate checks the FlowSpec for structural correctness. It verifies:
//   - Name is non-empty, alphanumeric+underscores, max 64 chars
//   - Description is non-empty
//   - At least one processor, max 20
//   - All processor IDs are unique
//   - All wiring rules reference valid processor IDs or the "input"/"output" boundaries
//   - No cycles exist in the processor wiring graph
func (f *FlowSpec) Validate() error {
	// Validate name.
	if f.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validNameRe.MatchString(f.Name) {
		return fmt.Errorf("name %q must be alphanumeric plus underscores and at most 64 characters", f.Name)
	}

	// Validate description.
	if f.Description == "" {
		return fmt.Errorf("description is required")
	}

	// Validate processor count.
	if len(f.Processors) == 0 {
		return fmt.Errorf("flow must contain at least one processor")
	}
	if len(f.Processors) > maxProcessors {
		return fmt.Errorf("flow exceeds maximum processor count (%d > %d)", len(f.Processors), maxProcessors)
	}

	// Build an index of processor IDs for O(1) membership checks and
	// duplicate detection.
	procIndex := make(map[string]struct{}, len(f.Processors))
	for _, p := range f.Processors {
		if p.ID == "" {
			return fmt.Errorf("processor ID must not be empty")
		}
		if !validNameRe.MatchString(p.ID) {
			return fmt.Errorf("processor ID %q must match [a-zA-Z0-9_] and be 1-64 chars", p.ID)
		}
		if p.Component == "" {
			return fmt.Errorf("processor %q: component must not be empty", p.ID)
		}
		if _, exists := procIndex[p.ID]; exists {
			return fmt.Errorf("duplicate processor ID %q", p.ID)
		}
		procIndex[p.ID] = struct{}{}
	}

	// Validate wiring references. A valid source prefix is either "input." or
	// "<proc-id>.output.". A valid destination prefix is either "<proc-id>.input."
	// or "output.".
	for i, w := range f.Wiring {
		if err := validateWiringFrom(w.From, procIndex, i); err != nil {
			return err
		}
		if err := validateWiringTo(w.To, procIndex, i); err != nil {
			return err
		}
	}

	// Check for cycles in the processor wiring graph. A directed edge A→B exists
	// when wiring routes from A's output to B's input. The "input" and "output"
	// boundaries are excluded from cycle detection — they are not processors.
	return detectWiringCycles(f.Wiring, procIndex)
}

// validateWiringFrom checks that a "from" value is either "input.<field>" or
// "<proc-id>.output.<field>".
func validateWiringFrom(from string, procIndex map[string]struct{}, idx int) error {
	prefix, _, ok := splitFirstSegment(from)
	if !ok {
		return fmt.Errorf("wiring[%d].from %q must have at least two segments separated by '.'", idx, from)
	}
	if prefix == "input" {
		return nil
	}
	// Must be <proc-id>.output.<field>
	if _, exists := procIndex[prefix]; !exists {
		return fmt.Errorf("wiring[%d].from %q references unknown processor ID %q", idx, from, prefix)
	}
	// Verify second segment is "output"
	_, rest, _ := splitFirstSegment(from)
	middle, _, hasMore := splitFirstSegment(rest)
	if middle != "output" || !hasMore {
		return fmt.Errorf("wiring[%d].from %q from a processor must follow the pattern '<id>.output.<field>'", idx, from)
	}
	return nil
}

// validateWiringTo checks that a "to" value is either "<proc-id>.input.<field>"
// or "output.<field>".
func validateWiringTo(to string, procIndex map[string]struct{}, idx int) error {
	prefix, rest, ok := splitFirstSegment(to)
	if !ok {
		return fmt.Errorf("wiring[%d].to %q must have at least two segments separated by '.'", idx, to)
	}
	if prefix == "output" {
		return nil
	}
	// Must be <proc-id>.input.<field>
	if _, exists := procIndex[prefix]; !exists {
		return fmt.Errorf("wiring[%d].to %q references unknown processor ID %q", idx, to, prefix)
	}
	middle, _, hasMore := splitFirstSegment(rest)
	if middle != "input" || !hasMore {
		return fmt.Errorf("wiring[%d].to %q to a processor must follow the pattern '<id>.input.<field>'", idx, to)
	}
	return nil
}

// detectWiringCycles builds a directed adjacency list from processor-to-processor
// wiring edges and runs a depth-first search to detect cycles.
// Edges are: A→B when a WiringRule routes from A.output.* to B.input.*.
func detectWiringCycles(wiring []WiringRule, procIndex map[string]struct{}) error {
	adj := make(map[string][]string, len(procIndex))
	// Initialise every processor with an empty edge list so isolated processors
	// are included in the DFS traversal.
	for id := range procIndex {
		adj[id] = nil
	}

	for _, w := range wiring {
		srcProc, _, _ := splitFirstSegment(w.From)
		dstProc, _, _ := splitFirstSegment(w.To)
		// Skip edges involving the "input" or "output" boundaries.
		_, srcIsProc := procIndex[srcProc]
		_, dstIsProc := procIndex[dstProc]
		if !srcIsProc || !dstIsProc {
			continue
		}
		if srcProc == dstProc {
			return fmt.Errorf("cycle detected: processor %q wires to itself", srcProc)
		}
		adj[srcProc] = append(adj[srcProc], dstProc)
	}

	// Three-colour DFS: white (0) = unvisited, gray (1) = in path, black (2) = done.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(adj))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, neighbour := range adj[id] {
			switch color[neighbour] {
			case gray:
				return fmt.Errorf("cycle detected: processor %q and processor %q are in a cycle", id, neighbour)
			case white:
				if err := visit(neighbour); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for id := range adj {
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}

	return nil
}

// splitFirstSegment splits "a.b.c" into ("a", "b.c", true).
// Returns ("", "", false) when there is no dot separator.
func splitFirstSegment(s string) (first, rest string, ok bool) {
	for i := range len(s) {
		if s[i] == '.' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
