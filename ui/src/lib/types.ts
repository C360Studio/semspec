/**
 * Main types entry point.
 *
 * Re-exports both UI-specific types and generated API types.
 * For direct access to generated types, see ./types/index.ts
 */

// ============================================================================
// UI-specific types (not from OpenAPI)
// ============================================================================

// UI-only message type for chat display (not from API)
export interface Message {
	id: string;
	type: 'user' | 'assistant' | 'status' | 'error';
	content: string;
	timestamp: string;
	loopId?: string;
}

// Stricter typing for loop states (generated type uses string)
export type LoopState = 'pending' | 'exploring' | 'executing' | 'paused' | 'complete' | 'success' | 'failed' | 'cancelled';

// Signal request body (not in OpenAPI response types)
export interface SignalRequest {
	type: 'pause' | 'resume' | 'cancel';
	reason?: string;
}

// System health types (not in generated OpenAPI)
export interface SystemHealth {
	healthy: boolean;
	components: ComponentHealth[];
}

export interface ComponentHealth {
	name: string;
	status: 'running' | 'stopped' | 'error';
	uptime: number;
}

// ============================================================================
// Re-export generated API types for backwards compatibility
// ============================================================================
export type {
	// Semstreams agentic-dispatch types
	Loop,
	MessageResponse,
	SignalResponse,
	ActivityEvent,
	// Semspec constitution types
	ConstitutionResponse,
	HTTPCheckRequest,
	HTTPCheckResponse,
	ReloadResponse,
	RulesResponse,
	SectionRulesResponse,
	Rule,
	RuleWithSection,
	Violation,
	// Runtime types
	RuntimeHealthResponse,
	RuntimeMessagesResponse,
	RuntimeMetricsResponse,
	// Flow types
	Flow,
	FlowStatusPayload,
	// Message types
	MessageLogEntry,
	LogEntryPayload,
	MetricsPayload,
	MetricEntry,
	// WebSocket types
	StatusStreamEnvelope,
	SubscribeCommand,
} from './types/index';
