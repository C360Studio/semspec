package reactive

import "encoding/json"

// JSON marshaling/unmarshaling for all payload and result types.
// Uses the standard Alias pattern to prevent infinite recursion while
// satisfying the message.Payload interface (which requires json.Marshaler
// and json.Unmarshaler).

// ---------------------------------------------------------------------------
// Request payload JSON methods
// ---------------------------------------------------------------------------

// MarshalJSON implements json.Marshaler.
func (r *PlannerRequest) MarshalJSON() ([]byte, error) {
	type Alias PlannerRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlannerRequest) UnmarshalJSON(data []byte) error {
	type Alias PlannerRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *PlanReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias PlanReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias PlanReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *PhaseGeneratorRequest) MarshalJSON() ([]byte, error) {
	type Alias PhaseGeneratorRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PhaseGeneratorRequest) UnmarshalJSON(data []byte) error {
	type Alias PhaseGeneratorRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *PhaseReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias PhaseReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PhaseReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias PhaseReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskGeneratorRequest) MarshalJSON() ([]byte, error) {
	type Alias TaskGeneratorRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskGeneratorRequest) UnmarshalJSON(data []byte) error {
	type Alias TaskGeneratorRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias TaskReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias TaskReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *DeveloperRequest) MarshalJSON() ([]byte, error) {
	type Alias DeveloperRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *DeveloperRequest) UnmarshalJSON(data []byte) error {
	type Alias DeveloperRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *ValidationRequest) MarshalJSON() ([]byte, error) {
	type Alias ValidationRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ValidationRequest) UnmarshalJSON(data []byte) error {
	type Alias ValidationRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskCodeReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias TaskCodeReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskCodeReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias TaskCodeReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ---------------------------------------------------------------------------
// Result type JSON methods
// ---------------------------------------------------------------------------

// MarshalJSON implements json.Marshaler.
func (r *PlannerResult) MarshalJSON() ([]byte, error) {
	type Alias PlannerResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlannerResult) UnmarshalJSON(data []byte) error {
	type Alias PlannerResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *ReviewResult) MarshalJSON() ([]byte, error) {
	type Alias ReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ReviewResult) UnmarshalJSON(data []byte) error {
	type Alias ReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *PhaseGeneratorResult) MarshalJSON() ([]byte, error) {
	type Alias PhaseGeneratorResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PhaseGeneratorResult) UnmarshalJSON(data []byte) error {
	type Alias PhaseGeneratorResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskGeneratorResult) MarshalJSON() ([]byte, error) {
	type Alias TaskGeneratorResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskGeneratorResult) UnmarshalJSON(data []byte) error {
	type Alias TaskGeneratorResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskReviewResult) MarshalJSON() ([]byte, error) {
	type Alias TaskReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskReviewResult) UnmarshalJSON(data []byte) error {
	type Alias TaskReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *ValidationResult) MarshalJSON() ([]byte, error) {
	type Alias ValidationResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ValidationResult) UnmarshalJSON(data []byte) error {
	type Alias ValidationResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *DeveloperResult) MarshalJSON() ([]byte, error) {
	type Alias DeveloperResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *DeveloperResult) UnmarshalJSON(data []byte) error {
	type Alias DeveloperResult
	return json.Unmarshal(data, (*Alias)(r))
}

// MarshalJSON implements json.Marshaler.
func (r *TaskCodeReviewResult) MarshalJSON() ([]byte, error) {
	type Alias TaskCodeReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskCodeReviewResult) UnmarshalJSON(data []byte) error {
	type Alias TaskCodeReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// ---------------------------------------------------------------------------
// Task execution event payload JSON methods (wrapper types delegate to inner struct)
// ---------------------------------------------------------------------------

// MarshalJSON implements json.Marshaler.
func (p *TaskValidationPassedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.StructuralValidationPassedEvent)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskValidationPassedPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.StructuralValidationPassedEvent)
}

// MarshalJSON implements json.Marshaler.
func (p *TaskRejectionCategorizedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.RejectionCategorizedEvent)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskRejectionCategorizedPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.RejectionCategorizedEvent)
}

// MarshalJSON implements json.Marshaler.
func (p *TaskCompletePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.TaskExecutionCompleteEvent)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskCompletePayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.TaskExecutionCompleteEvent)
}

// MarshalJSON implements json.Marshaler.
func (p *TaskExecEscalatePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.EscalationEvent)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskExecEscalatePayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.EscalationEvent)
}

// MarshalJSON implements json.Marshaler.
func (p *TaskExecErrorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.UserSignalErrorEvent)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskExecErrorPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.UserSignalErrorEvent)
}

// ---------------------------------------------------------------------------
// Task execution new-struct payload JSON methods (Alias pattern)
// ---------------------------------------------------------------------------

// MarshalJSON implements json.Marshaler.
func (p *PlanRefinementTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias PlanRefinementTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PlanRefinementTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias PlanRefinementTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// MarshalJSON implements json.Marshaler.
func (p *TaskDecompositionTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias TaskDecompositionTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskDecompositionTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias TaskDecompositionTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}
