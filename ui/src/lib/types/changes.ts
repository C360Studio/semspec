/**
 * Types for the changes/workflow management UI.
 * Based on semspec-ui-redesign-spec.md
 */

/**
 * State of a pipeline stage: proposal, design, spec, or tasks
 */
export type PipelineStageState = 'none' | 'generating' | 'complete' | 'failed';

/**
 * Workflow status values from backend
 */
export type WorkflowStatus =
	| 'created'
	| 'drafted'
	| 'reviewed'
	| 'approved'
	| 'implementing'
	| 'complete'
	| 'rejected';

/**
 * Loop associated with a change, showing active agent work
 */
export interface ActiveLoop {
	loop_id: string;
	role: string;
	model: string;
	state: string;
	iterations: number;
	max_iterations: number;
	workflow_step: string;
}

/**
 * GitHub integration metadata for a change
 */
export interface GitHubInfo {
	epic_number: number;
	epic_url: string;
	repository: string;
	task_issues: Record<string, number>;
}

/**
 * Task completion statistics
 */
export interface TaskStats {
	total: number;
	completed: number;
	failed: number;
	in_progress: number;
}

/**
 * File existence flags for workflow documents
 */
export interface WorkflowFiles {
	has_proposal: boolean;
	has_design: boolean;
	has_spec: boolean;
	has_tasks: boolean;
}

/**
 * Pipeline state derived from workflow files and current activity
 */
export interface PipelineState {
	proposal: PipelineStageState;
	design: PipelineStageState;
	spec: PipelineStageState;
	tasks: PipelineStageState;
}

/**
 * A change (workflow) with its full status and associated data.
 * This is the primary data type for the Board and Changes views.
 */
export interface ChangeWithStatus {
	slug: string;
	title: string;
	status: WorkflowStatus;
	author: string;
	created_at: string;
	updated_at: string;
	files: WorkflowFiles;
	github?: GitHubInfo;
	active_loops: ActiveLoop[];
	task_stats?: TaskStats;
}

/**
 * Attention item types
 */
export type AttentionType =
	| 'approval_needed'
	| 'question_pending'
	| 'task_failed'
	| 'task_blocked';

/**
 * An item requiring human attention
 */
export interface AttentionItem {
	type: AttentionType;
	change_slug?: string;
	loop_id?: string;
	title: string;
	description: string;
	action_url: string;
	created_at: string;
}

/**
 * Parsed task from tasks.md
 */
export interface ParsedTask {
	id: string;
	description: string;
	status: 'pending' | 'in_progress' | 'complete' | 'failed' | 'blocked';
	blocked_by?: string[];
	assigned_loop_id?: string;
	github_issue?: number;
}

/**
 * Document metadata for workflow documents
 */
export interface DocumentInfo {
	type: 'proposal' | 'design' | 'spec' | 'tasks';
	exists: boolean;
	content?: string;
	generated_at?: string;
	model?: string;
}

/**
 * Helper to derive pipeline state from files and active loops
 */
export function derivePipelineState(
	files: WorkflowFiles,
	activeLoops: ActiveLoop[]
): PipelineState {
	// Check if any loop is generating a specific stage
	const generatingStep = activeLoops.find((l) => l.state === 'executing')?.workflow_step;

	return {
		proposal: files.has_proposal
			? 'complete'
			: generatingStep === 'proposal'
				? 'generating'
				: 'none',
		design: files.has_design
			? 'complete'
			: generatingStep === 'design'
				? 'generating'
				: 'none',
		spec: files.has_spec
			? 'complete'
			: generatingStep === 'spec'
				? 'generating'
				: 'none',
		tasks: files.has_tasks
			? 'complete'
			: generatingStep === 'tasks'
				? 'generating'
				: 'none'
	};
}
