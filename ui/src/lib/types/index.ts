/**
 * Generated API types from OpenAPI specifications.
 *
 * Types are auto-generated from the OpenAPI specs - do not edit the generated files directly.
 *
 * To regenerate types:
 *   npm run generate:types         # semspec types
 *   npm run generate:types:semstreams  # semstreams types
 */

// Re-export semspec API types
export type {
	paths,
	components,
	operations,
} from './api.generated';

// Export commonly used schema types for convenience
export type {
	components as SemspecComponents,
} from './api.generated';

// Export semstreams types under a namespace to avoid conflicts
export type {
	paths as SemstreamsPaths,
	components as SemstreamsComponents,
} from './semstreams.generated';

// ============================================================================
// Semspec API types
// ============================================================================
import type { components } from './api.generated';

export type Standard = components['schemas']['Standard'];

// Runtime types
export type RuntimeHealthResponse = components['schemas']['RuntimeHealthResponse'];
export type RuntimeMessagesResponse = components['schemas']['RuntimeMessagesResponse'];
export type RuntimeMetricsResponse = components['schemas']['RuntimeMetricsResponse'];

// Flow types
export type Flow = components['schemas']['Flow'];
export type FlowStatusPayload = components['schemas']['FlowStatusPayload'];

// Message types
export type MessageLogEntry = components['schemas']['MessageLogEntry'];
export type LogEntryPayload = components['schemas']['LogEntryPayload'];
export type MetricsPayload = components['schemas']['MetricsPayload'];
export type MetricEntry = components['schemas']['MetricEntry'];

// WebSocket types
export type StatusStreamEnvelope = components['schemas']['StatusStreamEnvelope'];
export type SubscribeCommand = components['schemas']['SubscribeCommand'];

// ============================================================================
// Workflow API types (plan lifecycle)
// ============================================================================

export type GeneratedPlanWithStatus = components['schemas']['PlanWithStatus'];
export type GeneratedActiveLoopStatus = components['schemas']['ActiveLoopStatus'];
// Task/AcceptanceCriterion schemas removed from backend — see types/task.ts
export type GeneratedCreatePlanResponse = components['schemas']['CreatePlanResponse'];
export type GeneratedAsyncOperationResponse = components['schemas']['AsyncOperationResponse'];
export type GeneratedSynthesisResult = components['schemas']['SynthesisResult'];
export type GeneratedReviewerSummary = components['schemas']['ReviewerSummary'];
export type GeneratedSynthesisStats = components['schemas']['SynthesisStats'];
export type GeneratedReviewFinding = components['schemas']['ReviewFinding'];

// ============================================================================
// Semstreams API types (agentic-dispatch)
// ============================================================================
import type { components as semstreamsComponents } from './semstreams.generated';

// Alias LoopInfo to Loop for backwards compatibility with existing code
export type Loop = semstreamsComponents['schemas']['LoopInfo'];

// Alias HTTPMessageResponse to MessageResponse for backwards compatibility
export type MessageResponse = semstreamsComponents['schemas']['HTTPMessageResponse'];

// Signal response from agentic-dispatch
export type SignalResponse = semstreamsComponents['schemas']['SignalResponse'];

// Activity events from SSE stream
export type ActivityEvent = semstreamsComponents['schemas']['ActivityEvent'];

// ============================================================================
// ADR-024 graph topology types
// ============================================================================

export type { Requirement, RequirementStatus } from './requirement';
export { getRequirementStatusInfo } from './requirement';

export type { Scenario, ScenarioStatus } from './scenario';
export { getScenarioStatusInfo } from './scenario';

export type { ChangeProposal, ChangeProposalStatus } from './change-proposal';
export { getChangeProposalStatusInfo } from './change-proposal';

// ============================================================================
// Question types (Knowledge Gap Resolution Protocol)
// ============================================================================

export type QuestionStatus = 'pending' | 'answered' | 'timeout';
export type QuestionUrgency = 'low' | 'normal' | 'high' | 'blocking';

export interface AnswerAction {
	type: 'install_package' | 'suggest_alternative' | 'none';
	parameters: Record<string, string>;
}

export interface Question {
	id: string;
	from_agent: string;
	topic: string;
	question: string;
	category?: 'knowledge' | 'environment' | 'approval';
	context?: string;
	metadata?: Record<string, string>;
	blocked_loop_id?: string;
	plan_slug?: string;
	urgency: QuestionUrgency;
	status: QuestionStatus;
	created_at: string;
	deadline?: string;
	answered_at?: string;
	answer?: string;
	answered_by?: string;
	answerer_type?: 'agent' | 'team' | 'human';
	confidence?: 'high' | 'medium' | 'low';
	sources?: string;
	action?: AnswerAction;
}
