/**
 * FeedEvent — normalized event from plan, execution, or question SSE sources.
 * Used by the left-panel Activity Feed for a unified lifecycle view.
 */
export type FeedEvent = {
	/** Dedup key (source + SSE event ID or timestamp) */
	id: string;
	timestamp: string;
	source: 'plan' | 'execution' | 'question';
	/** Original SSE event type (plan_updated, task_updated, question_created, etc.) */
	type: string;
	/** Human-readable summary */
	summary: string;
	/** Plan slug for filtering */
	slug?: string;
	/** Raw payload for drill-down */
	data?: Record<string, unknown>;
};

/** Subset of PlanWithStatus relevant for feed display */
export type PlanSSEPayload = {
	slug: string;
	stage: string;
	goal?: string;
	approved?: boolean;
	requirements?: { id: string; title: string }[];
	scenarios?: { id: string }[];
};

/** Task execution payload from execution SSE */
export type TaskSSEPayload = {
	entity_id: string;
	slug: string;
	task_id: string;
	stage: string;
	title: string;
	iteration: number;
	max_iterations: number;
	loop_id?: string;
	tests_passed?: boolean;
	validation_passed?: boolean;
	verdict?: string;
	feedback?: string;
	/** Current TDD cycle (0-indexed) — reveals how many rework loops this node has burned */
	tdd_cycle?: number;
	/** Hard cap on TDD cycles before escalation */
	max_tdd_cycles?: number;
	/** How many times the reviewer was re-dispatched because its output couldn't be parsed */
	review_retry_count?: number;
	/** ISO timestamp of last state mutation — used to surface "is this still moving?" */
	updated_at?: string;
};

/** Requirement execution payload from execution SSE */
export type RequirementSSEPayload = {
	entity_id: string;
	slug: string;
	requirement_id: string;
	stage: string;
	title: string;
	node_count?: number;
	current_node_idx?: number;
	loop_id?: string;
	review_verdict?: string;
	/** ISO timestamp of last state mutation — used to surface liveness */
	updated_at?: string;
};
