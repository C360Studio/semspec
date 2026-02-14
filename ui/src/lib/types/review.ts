/**
 * Types for the two-stage review system.
 * Matches workflow/prompts/reviewers.go, workflow/prompts/spec_reviewer.go,
 * and processor/synthesis-aggregator/types.go
 */

// =============================================================================
// Verdict and Severity Types
// =============================================================================

/** Overall synthesis verdict */
export type SynthesisVerdict = 'approved' | 'rejected' | 'needs_changes';

/** Spec compliance verdicts (Stage 1 gate) */
export type SpecVerdict =
	| 'compliant'
	| 'over_built'
	| 'under_built'
	| 'wrong_scope'
	| 'multiple_issues';

/** Severity levels for findings */
export type Severity = 'critical' | 'high' | 'medium' | 'low';

/** Spec finding types */
export type SpecFindingType = 'over_built' | 'under_built' | 'wrong_scope';

// =============================================================================
// Finding Types
// =============================================================================

/**
 * Spec finding from Stage 1 spec compliance review.
 * Matches workflow/prompts/spec_reviewer.go SpecFinding
 */
export interface SpecFinding {
	type: SpecFindingType;
	severity: Severity;
	description: string;
	file?: string;
	lines?: string; // e.g., "45-67"
	spec_reference?: string;
}

/**
 * Review finding from Stage 2 quality reviewers (SOP/Style/Security).
 * Matches workflow/prompts/reviewers.go ReviewFinding
 */
export interface ReviewFinding {
	role: string; // sop_reviewer, style_reviewer, security_reviewer, spec_reviewer
	category: string; // naming, injection, sop_id, over_built, etc.
	severity: Severity;
	file: string;
	line: number;
	issue: string;
	suggestion: string;
	sop_id?: string; // For SOP findings
	status?: string; // violated, passed, not_applicable
	cwe?: string; // CWE-89 for security
}

// =============================================================================
// Reviewer Summary
// =============================================================================

/**
 * Per-reviewer summary in synthesis result.
 * Matches processor/synthesis-aggregator/types.go ReviewerSummary
 */
export interface ReviewerSummary {
	role: string;
	passed: boolean;
	summary: string;
	finding_count: number;
	verdict?: string; // Only for spec_reviewer
}

// =============================================================================
// Synthesis Statistics
// =============================================================================

/**
 * Aggregation statistics for synthesis result.
 * Matches processor/synthesis-aggregator/types.go SynthesisStats
 */
export interface SynthesisStats {
	total_findings: number;
	by_severity: Record<string, number>;
	by_reviewer: Record<string, number>;
	reviewers_total: number;
	reviewers_passed: number;
}

// =============================================================================
// Synthesis Result (Main Type)
// =============================================================================

/**
 * Final aggregated result from all reviewers.
 * Matches processor/synthesis-aggregator/types.go SynthesisResult
 */
export interface SynthesisResult {
	request_id: string;
	workflow_id?: string;
	verdict: SynthesisVerdict;
	passed: boolean;
	findings: ReviewFinding[];
	reviewers: ReviewerSummary[];
	summary: string;
	stats: SynthesisStats;
	partial?: boolean; // True if timeout before all reported
	missing_reviewers?: string[];
	error?: string;
}

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Get display label for a severity level
 */
export function getSeverityLabel(severity: Severity): string {
	return severity.charAt(0).toUpperCase() + severity.slice(1);
}

/**
 * Get CSS class for severity (maps to badge-* classes)
 */
export function getSeverityClass(severity: Severity): string {
	switch (severity) {
		case 'critical':
			return 'error';
		case 'high':
			return 'warning';
		case 'medium':
			return 'info';
		case 'low':
			return 'neutral';
	}
}

/**
 * Get display label for a verdict
 */
export function getVerdictLabel(verdict: SynthesisVerdict | SpecVerdict): string {
	switch (verdict) {
		case 'approved':
			return 'Approved';
		case 'rejected':
			return 'Rejected';
		case 'needs_changes':
			return 'Needs Changes';
		case 'compliant':
			return 'Compliant';
		case 'over_built':
			return 'Over Built';
		case 'under_built':
			return 'Under Built';
		case 'wrong_scope':
			return 'Wrong Scope';
		case 'multiple_issues':
			return 'Multiple Issues';
		default:
			return verdict;
	}
}

/**
 * Get CSS class for verdict (maps to badge-* classes)
 */
export function getVerdictClass(verdict: SynthesisVerdict | SpecVerdict): string {
	switch (verdict) {
		case 'approved':
		case 'compliant':
			return 'success';
		case 'rejected':
		case 'over_built':
		case 'under_built':
		case 'wrong_scope':
		case 'multiple_issues':
			return 'error';
		case 'needs_changes':
			return 'warning';
		default:
			return 'neutral';
	}
}

/**
 * Get display label for a reviewer role
 */
export function getReviewerLabel(role: string): string {
	switch (role) {
		case 'spec_reviewer':
			return 'Spec Compliance';
		case 'sop_reviewer':
			return 'SOP';
		case 'style_reviewer':
			return 'Style';
		case 'security_reviewer':
			return 'Security';
		default:
			return role.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
	}
}

/**
 * Sort findings by severity (critical first)
 */
export function sortFindingsBySeverity(findings: ReviewFinding[]): ReviewFinding[] {
	const severityOrder: Record<Severity, number> = {
		critical: 0,
		high: 1,
		medium: 2,
		low: 3
	};

	return [...findings].sort((a, b) => {
		const aOrder = severityOrder[a.severity as Severity] ?? 99;
		const bOrder = severityOrder[b.severity as Severity] ?? 99;
		return aOrder - bOrder;
	});
}

/**
 * Group findings by file
 */
export function groupFindingsByFile(findings: ReviewFinding[]): Record<string, ReviewFinding[]> {
	const grouped: Record<string, ReviewFinding[]> = {};

	for (const finding of findings) {
		const file = finding.file || '(no file)';
		if (!grouped[file]) {
			grouped[file] = [];
		}
		grouped[file].push(finding);
	}

	// Sort each group by line number
	for (const file of Object.keys(grouped)) {
		grouped[file].sort((a, b) => (a.line || 0) - (b.line || 0));
	}

	return grouped;
}

/**
 * Check if the spec gate passed (Stage 1)
 */
export function isSpecGatePassed(result: SynthesisResult): boolean {
	const specReviewer = result.reviewers.find((r) => r.role === 'spec_reviewer');
	return specReviewer?.passed ?? false;
}

/**
 * Get the spec reviewer from synthesis result
 */
export function getSpecReviewer(result: SynthesisResult): ReviewerSummary | undefined {
	return result.reviewers.find((r) => r.role === 'spec_reviewer');
}

/**
 * Get quality reviewers (Stage 2 - excludes spec_reviewer)
 */
export function getQualityReviewers(result: SynthesisResult): ReviewerSummary[] {
	return result.reviewers.filter((r) => r.role !== 'spec_reviewer');
}
