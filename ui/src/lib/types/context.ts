/**
 * Types for the context builder system.
 * Matches processor/context-builder/types.go
 */

// =============================================================================
// Task and Provenance Types
// =============================================================================

/** Task types for context building */
export type ContextTaskType = 'review' | 'implementation' | 'exploration';

/** Provenance types categorizing context sources */
export type ProvenanceType =
	| 'sop'
	| 'git_diff'
	| 'file'
	| 'entity'
	| 'graph'
	| 'summary'
	| 'spec'
	| 'test'
	| 'convention';

// =============================================================================
// Provenance Entry
// =============================================================================

/**
 * Tracks where a context item came from.
 * Matches processor/context-builder/types.go ProvenanceEntry
 */
export interface ProvenanceEntry {
	/** Source identifier (e.g., "sop:entity-id", "git:HEAD~1..HEAD", "file:/path") */
	source: string;
	/** Type of source */
	type: ProvenanceType;
	/** Token count for this item */
	tokens: number;
	/** Whether this item was truncated to fit budget */
	truncated?: boolean;
	/** Allocation priority (lower = higher priority) */
	priority?: number;
}

// =============================================================================
// Entity Reference
// =============================================================================

/**
 * Reference to a graph entity in the context.
 * Matches processor/context-builder/types.go EntityRef
 */
export interface EntityRef {
	/** Entity identifier */
	id: string;
	/** Entity type (sop, function, type, spec, pattern) */
	type?: string;
	/** Hydrated entity content (optional) */
	content?: string;
	/** Token count for this entity */
	tokens?: number;
}

// =============================================================================
// Context Build Response (Main Type)
// =============================================================================

/**
 * Response from context builder.
 * Matches processor/context-builder/types.go ContextBuildResponse
 */
export interface ContextBuildResponse {
	/** Request ID this response is for */
	request_id: string;
	/** Task type from the request */
	task_type: ContextTaskType;
	/** Total tokens in the built context */
	token_count: number;
	/** Graph entities included in context */
	entities?: EntityRef[];
	/** File contents by path */
	documents?: Record<string, string>;
	/** Git diff content */
	diffs?: string;
	/** Provenance tracking for all included items */
	provenance?: ProvenanceEntry[];
	/** SOP entity IDs included (for review validation) */
	sop_ids?: string[];
	/** Actual tokens used */
	tokens_used: number;
	/** Total budget available */
	tokens_budget: number;
	/** Whether content was truncated to fit budget */
	truncated: boolean;
	/** Error message if context building failed */
	error?: string;
}

// =============================================================================
// Context Build Request (for reference)
// =============================================================================

/**
 * Request to build context.
 * Matches processor/context-builder/types.go ContextBuildRequest
 */
export interface ContextBuildRequest {
	/** Unique request identifier */
	request_id: string;
	/** Task type determining strategy */
	task_type: ContextTaskType;
	/** Optional workflow association */
	workflow_id?: string;
	/** Changed files (for review tasks) */
	files?: string[];
	/** Git commit/branch reference (for review tasks) */
	git_ref?: string;
	/** Search topic (for exploration tasks) */
	topic?: string;
	/** Specification entity ID (for implementation tasks) */
	spec_entity_id?: string;
	/** Model capability for budget calculation */
	capability?: string;
	/** Explicit model override */
	model?: string;
	/** Explicit token budget override */
	token_budget?: number;
}

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Get display label for a provenance type
 */
export function getProvenanceLabel(type: ProvenanceType): string {
	switch (type) {
		case 'sop':
			return 'SOP';
		case 'git_diff':
			return 'Git Diff';
		case 'file':
			return 'File';
		case 'entity':
			return 'Entity';
		case 'graph':
			return 'Graph';
		case 'summary':
			return 'Summary';
		case 'spec':
			return 'Spec';
		case 'test':
			return 'Test';
		case 'convention':
			return 'Convention';
		default:
			return type;
	}
}

/**
 * Get icon name for a provenance type (lucide icons)
 */
export function getProvenanceIcon(type: ProvenanceType): string {
	switch (type) {
		case 'sop':
			return 'book-open';
		case 'git_diff':
			return 'git-commit';
		case 'file':
			return 'file';
		case 'entity':
			return 'database';
		case 'graph':
			return 'layers';
		case 'summary':
			return 'file-text';
		case 'spec':
			return 'list-checks';
		case 'test':
			return 'check-circle';
		case 'convention':
			return 'code';
		default:
			return 'file';
	}
}

/**
 * Get display label for a task type
 */
export function getTaskTypeLabel(type: ContextTaskType): string {
	switch (type) {
		case 'review':
			return 'Review';
		case 'implementation':
			return 'Implementation';
		case 'exploration':
			return 'Exploration';
		default:
			return type;
	}
}

/**
 * Calculate budget usage percentage
 */
export function getBudgetPercent(response: ContextBuildResponse): number {
	if (response.tokens_budget <= 0) return 0;
	return Math.round((response.tokens_used / response.tokens_budget) * 100);
}

/**
 * Format token count for display
 */
export function formatTokens(tokens: number): string {
	if (tokens >= 1000) {
		return `${(tokens / 1000).toFixed(1)}k`;
	}
	return tokens.toString();
}

/**
 * Sort provenance entries by priority
 */
export function sortProvenanceByPriority(entries: ProvenanceEntry[]): ProvenanceEntry[] {
	return [...entries].sort((a, b) => (a.priority ?? 99) - (b.priority ?? 99));
}

/**
 * Group provenance entries by type
 */
export function groupProvenanceByType(
	entries: ProvenanceEntry[]
): Record<ProvenanceType, ProvenanceEntry[]> {
	const grouped: Partial<Record<ProvenanceType, ProvenanceEntry[]>> = {};

	for (const entry of entries) {
		if (!grouped[entry.type]) {
			grouped[entry.type] = [];
		}
		grouped[entry.type]!.push(entry);
	}

	return grouped as Record<ProvenanceType, ProvenanceEntry[]>;
}

/**
 * Get total tokens from provenance entries
 */
export function getTotalTokensFromProvenance(entries: ProvenanceEntry[]): number {
	return entries.reduce((sum, entry) => sum + entry.tokens, 0);
}

/**
 * Check if any provenance items were truncated
 */
export function hasAnyTruncated(entries: ProvenanceEntry[]): boolean {
	return entries.some((entry) => entry.truncated);
}

/**
 * Extract short name from source string
 * e.g., "sop:error-handling" -> "error-handling"
 * e.g., "file:/path/to/file.go" -> "file.go"
 */
export function getSourceShortName(source: string): string {
	const parts = source.split(':');
	if (parts.length < 2) return source;

	const value = parts.slice(1).join(':');

	// For file paths, get just the filename
	if (parts[0] === 'file') {
		const pathParts = value.split('/');
		return pathParts[pathParts.length - 1];
	}

	return value;
}
