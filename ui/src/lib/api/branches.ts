import { request } from './client';

/**
 * One file changed on a requirement branch. Mirrors the backend shape from
 * `processor/plan-manager/workspace.go:BranchDiffFile`.
 */
export interface BranchDiffFile {
	path: string;
	old_path?: string;
	status: 'added' | 'modified' | 'deleted' | 'renamed' | 'copied' | 'typechange' | 'binary';
	insertions: number;
	deletions: number;
	binary?: boolean;
}

/**
 * Per-requirement branch summary: the plan's requirement joined with the git
 * diff of its branch against the base. This is the authoritative data source
 * for the Files view — one row per requirement, listing what the agent
 * actually changed on that requirement's branch.
 */
export interface PlanRequirementBranch {
	requirement_id: string;
	title: string;
	branch: string;
	stage: string;
	base: string;
	files: BranchDiffFile[];
	total_insertions: number;
	total_deletions: number;
	/** Set when the sandbox branch-diff call failed. UI should surface this. */
	diff_error?: string;
	/** Last reviewer verdict text. Populated after at least one review cycle. */
	review_feedback?: string;
	/** Structured reviewer verdict (approved / needs_changes / fixable / etc). */
	review_verdict?: string;
	/** Executor error tag when the requirement failed outside a review cycle. */
	error_reason?: string;
	/** Retries already burned against max_retries. */
	retry_count?: number;
	max_retries?: number;
}

export async function fetchPlanBranches(slug: string): Promise<PlanRequirementBranch[]> {
	return request<PlanRequirementBranch[]>(
		`/plan-manager/plans/${encodeURIComponent(slug)}/branches`
	);
}

export async function fetchRequirementFileDiff(
	slug: string,
	requirementId: string,
	path: string
): Promise<string> {
	const result = await request<{ patch: string }>(
		`/plan-manager/plans/${encodeURIComponent(slug)}/requirements/${encodeURIComponent(
			requirementId
		)}/file-diff?path=${encodeURIComponent(path)}`
	);
	return result.patch;
}
