/**
 * Types for trajectory viewing — agent loop execution history.
 *
 * Trajectory data comes from agentic-loop component (semstreams alpha.79+).
 * List endpoint: GET /agentic-loop/trajectories
 * Detail endpoint: GET /agentic-loop/trajectories/{loopId}
 */

/** TrajectoryStep represents a single step in an agentic trajectory. */
export interface TrajectoryStep {
	timestamp: string;
	step_type: 'model_call' | 'tool_call' | 'context_compaction';
	request_id?: string;
	prompt?: string;
	response?: string;
	tokens_in?: number;
	tokens_out?: number;
	tool_name?: string;
	tool_arguments?: Record<string, unknown>;
	tool_result?: string;
	duration: number; // milliseconds
	messages?: ChatMessage[];
	tool_calls?: ToolCallRef[];
	model?: string;
	provider?: string;
	capability?: string;
	retry_count?: number;
	utilization?: number;
	// UI-only: legacy compat aliases
	error?: string;
}

/** ChatMessage from the LLM conversation (detail=full). */
export interface ChatMessage {
	role: string;
	content: string;
}

/** ToolCall reference from assistant response (detail=full). */
export interface ToolCallRef {
	id: string;
	type: string;
	function: {
		name: string;
		arguments: string;
	};
}

/** Full trajectory with all steps for a single loop. */
export interface Trajectory {
	loop_id: string;
	start_time: string;
	end_time?: string;
	steps: TrajectoryStep[];
	outcome?: string;
	total_tokens_in: number;
	total_tokens_out: number;
	duration: number; // milliseconds
}

/** Summary item for trajectory list responses. */
export interface TrajectoryListItem {
	loop_id: string;
	task_id: string;
	outcome?: string;
	role: string;
	model: string;
	workflow_slug?: string;
	workflow_step?: string;
	iterations: number;
	total_tokens_in: number;
	total_tokens_out: number;
	duration: number; // milliseconds
	start_time: string;
	end_time?: string;
	metadata?: Record<string, unknown>;
}

/** Response from GET /agentic-loop/trajectories. */
export interface TrajectoryListResponse {
	trajectories: TrajectoryListItem[];
	total: number;
}

/** Filter parameters for trajectory list queries. */
export interface TrajectoryFilter {
	outcome?: string;
	role?: string;
	workflow_slug?: string;
	since?: string;
	metadata_key?: string;
	metadata_value?: string;
	limit?: number;
	offset?: number;
}

// ---------------------------------------------------------------------------
// Legacy compat: TrajectoryEntry is now TrajectoryStep
// ---------------------------------------------------------------------------
export type TrajectoryEntry = TrajectoryStep;
