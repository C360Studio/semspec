/**
 * Types for trajectory viewing — agent loop execution history.
 *
 * Trajectory data comes from trajectory-api which aggregates from
 * LLM_CALLS and TOOL_CALLS KV buckets.
 */

/** Trajectory represents aggregated data about an agent loop's execution history. */
export interface Trajectory {
	loop_id: string;
	trace_id?: string;
	steps: number;
	tool_calls: number;
	model_calls: number;
	tokens_in: number;
	tokens_out: number;
	duration_ms: number;
	status?: string;
	started_at?: string;
	ended_at?: string;
	entries?: TrajectoryEntry[];
}

/** TrajectoryEntry represents a single event in the trajectory timeline. */
export interface TrajectoryEntry {
	type: 'model_call' | 'tool_call';
	timestamp: string;
	duration_ms?: number;
	// model_call fields
	model?: string;
	provider?: string;
	capability?: string;
	tokens_in?: number;
	tokens_out?: number;
	finish_reason?: string;
	messages_count?: number;
	response_preview?: string;
	// tool_call fields
	tool_name?: string;
	status?: string;
	result_preview?: string;
	// shared
	error?: string;
	retries?: number;
}
