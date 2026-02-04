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
// Semspec API types (constitution, etc.)
// ============================================================================
import type { components } from './api.generated';

export type ConstitutionResponse = components['schemas']['ConstitutionResponse'];
export type HTTPCheckRequest = components['schemas']['HTTPCheckRequest'];
export type HTTPCheckResponse = components['schemas']['HTTPCheckResponse'];
export type ReloadResponse = components['schemas']['ReloadResponse'];
export type RulesResponse = components['schemas']['RulesResponse'];
export type SectionRulesResponse = components['schemas']['SectionRulesResponse'];
export type Rule = components['schemas']['Rule'];
export type RuleWithSection = components['schemas']['RuleWithSection'];
export type Violation = components['schemas']['Violation'];

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
