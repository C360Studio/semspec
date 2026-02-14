import { api } from '$lib/api/client';
import type {
	SynthesisResult,
	ReviewFinding,
	ReviewerSummary,
	SynthesisStats
} from '$lib/types/review';
import {
	sortFindingsBySeverity,
	groupFindingsByFile,
	isSpecGatePassed,
	getSpecReviewer,
	getQualityReviewers
} from '$lib/types/review';

/**
 * Store for managing review synthesis results.
 * Fetches and caches SynthesisResult for plans.
 */
class ReviewsStore {
	/** Cached results by plan slug */
	resultsByPlan = $state<Record<string, SynthesisResult>>({});

	/** Loading state */
	loading = $state(false);

	/** Error state */
	error = $state<string | null>(null);

	/**
	 * Fetch review results for a plan
	 */
	async fetch(slug: string): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			const result = await api.plans.getReviews(slug);
			this.resultsByPlan[slug] = result;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch reviews';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Get cached result for a plan
	 */
	get(slug: string): SynthesisResult | undefined {
		return this.resultsByPlan[slug];
	}

	/**
	 * Check if result exists and is loaded
	 */
	has(slug: string): boolean {
		return slug in this.resultsByPlan;
	}

	/**
	 * Get findings sorted by severity
	 */
	getFindings(slug: string): ReviewFinding[] {
		const result = this.resultsByPlan[slug];
		if (!result) return [];
		return sortFindingsBySeverity(result.findings);
	}

	/**
	 * Get findings grouped by file
	 */
	getByFile(slug: string): Record<string, ReviewFinding[]> {
		const result = this.resultsByPlan[slug];
		if (!result) return {};
		return groupFindingsByFile(result.findings);
	}

	/**
	 * Check if spec gate passed (Stage 1)
	 */
	isGatePassed(slug: string): boolean {
		const result = this.resultsByPlan[slug];
		if (!result) return false;
		return isSpecGatePassed(result);
	}

	/**
	 * Get spec reviewer summary (Stage 1)
	 */
	getSpecReviewer(slug: string): ReviewerSummary | undefined {
		const result = this.resultsByPlan[slug];
		if (!result) return undefined;
		return getSpecReviewer(result);
	}

	/**
	 * Get quality reviewers (Stage 2)
	 */
	getQualityReviewers(slug: string): ReviewerSummary[] {
		const result = this.resultsByPlan[slug];
		if (!result) return [];
		return getQualityReviewers(result);
	}

	/**
	 * Get statistics
	 */
	getStats(slug: string): SynthesisStats | undefined {
		const result = this.resultsByPlan[slug];
		return result?.stats;
	}

	/**
	 * Clear cached result for a plan
	 */
	clear(slug: string): void {
		delete this.resultsByPlan[slug];
	}

	/**
	 * Clear all cached results
	 */
	clearAll(): void {
		this.resultsByPlan = {};
	}
}

export const reviewsStore = new ReviewsStore();
