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

// Extended loop info with workflow context (pending semstreams API additions)
export interface LoopWithContext {
	loop_id: string;
	task_id: string;
	user_id: string;
	channel_type: string;
	channel_id: string;
	state: LoopState | string;
	iterations: number;
	max_iterations: number;
	created_at: string;
	// Pending additions from semstreams API:
	role?: string;           // proposal-writer, design-writer, spec-writer, tasks-writer
	workflow_slug?: string;  // add-user-auth
	workflow_step?: string;  // propose, design, spec, tasks
	model?: string;          // qwen, claude-sonnet
}

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

// Entity browser types
export type EntityType = 'code' | 'proposal' | 'spec' | 'task' | 'loop' | 'activity';

export interface Entity {
	id: string;
	type: EntityType;
	name: string;
	predicates: Record<string, unknown>;
	createdAt?: string;
	updatedAt?: string;
}

export interface Relationship {
	predicate: string;
	predicateLabel: string;
	targetId: string;
	targetType: EntityType;
	targetName: string;
	direction: 'outgoing' | 'incoming';
}

export interface EntityListParams extends Record<string, unknown> {
	type?: EntityType;
	query?: string;
	limit?: number;
	offset?: number;
}

export interface EntityWithRelationships extends Entity {
	relationships: Relationship[];
}

// BFO/CCO classification badges
export interface OntologyClassification {
	bfo?: string; // BFO class (e.g., 'GenericallyDependentContinuant')
	cco?: string; // CCO class (e.g., 'SoftwareCode')
	prov?: string; // PROV class (e.g., 'Entity', 'Activity')
}

// Provenance chain for an entity
export interface ProvenanceChain {
	generatedBy?: string; // Activity that generated this entity
	derivedFrom?: string[]; // Entities this was derived from
	attributedTo?: string[]; // Agents attributed to this entity
	usedBy?: string[]; // Activities that used this entity
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
	// Question types
	Question,
	QuestionStatus,
	QuestionUrgency,
} from './types/index';
