/**
 * Trajectory types — re-exported from generated semstreams schema.
 * Source: semstreams v1.0.0-alpha.82 OpenAPI spec
 *
 * List endpoint: GET /agentic-loop/trajectories
 * Detail endpoint: GET /agentic-loop/trajectories/{loopId}
 */
import type { components } from './semstreams.generated';

// Step type extended with UI-only error field
export type TrajectoryStep = components['schemas']['TrajectoryStep'] & {
	/** UI extension: error message if step failed (not in semstreams spec) */
	error?: string;
};

// Trajectory with extended steps
export type Trajectory = Omit<components['schemas']['Trajectory'], 'steps'> & {
	steps: TrajectoryStep[];
};
export type TrajectoryListItem = components['schemas']['TrajectoryListItem'];
export type TrajectoryListResponse = components['schemas']['TrajectoryListResponse'];

// Inner types extracted from TrajectoryStep for component use
export type ChatMessage = NonNullable<TrajectoryStep['messages']>[number];
export type ToolCallRef = NonNullable<TrajectoryStep['tool_calls']>[number];

// Filter params (from generated paths — semstreams spec defines these as query params)
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

// Legacy compat alias
export type TrajectoryEntry = TrajectoryStep;
