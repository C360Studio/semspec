import { api } from '$lib/api/client';
import type { ContextBuildResponse, ProvenanceEntry } from '$lib/types/context';
import { sortProvenanceByPriority, getBudgetPercent } from '$lib/types/context';

/**
 * Store for managing context build responses.
 * Fetches and caches ContextBuildResponse by request ID.
 *
 * Note: The Loop struct should have a `context_request_id` field
 * that links to the context build request.
 */
class ContextStore {
	/** Cached responses by request ID */
	responses = $state<Record<string, ContextBuildResponse>>({});

	/** Loading state per request */
	loading = $state<Record<string, boolean>>({});

	/** Error state per request */
	errors = $state<Record<string, string | null>>({});

	/**
	 * Fetch context by request ID
	 */
	async fetch(requestId: string): Promise<void> {
		this.loading[requestId] = true;
		this.errors[requestId] = null;

		try {
			const response = await api.context.get(requestId);
			this.responses[requestId] = response;
		} catch (err) {
			this.errors[requestId] = err instanceof Error ? err.message : 'Failed to fetch context';
		} finally {
			this.loading[requestId] = false;
		}
	}

	/**
	 * Check if loading for a specific request
	 */
	isLoading(requestId: string): boolean {
		return this.loading[requestId] ?? false;
	}

	/**
	 * Get error for a specific request
	 */
	getError(requestId: string): string | null {
		return this.errors[requestId] ?? null;
	}

	/**
	 * Get cached response by request ID
	 */
	get(requestId: string): ContextBuildResponse | undefined {
		return this.responses[requestId];
	}

	/**
	 * Check if response exists for a request
	 */
	has(requestId: string): boolean {
		return requestId in this.responses;
	}

	/**
	 * Get provenance entries sorted by priority
	 */
	getProvenance(requestId: string): ProvenanceEntry[] {
		const response = this.responses[requestId];
		if (!response?.provenance) return [];
		return sortProvenanceByPriority(response.provenance);
	}

	/**
	 * Get budget usage info
	 */
	getBudgetUsage(requestId: string): { used: number; budget: number; percent: number } {
		const response = this.responses[requestId];
		if (!response) {
			return { used: 0, budget: 0, percent: 0 };
		}
		return {
			used: response.tokens_used,
			budget: response.tokens_budget,
			percent: getBudgetPercent(response)
		};
	}

	/**
	 * Check if any content was truncated
	 */
	isTruncated(requestId: string): boolean {
		const response = this.responses[requestId];
		return response?.truncated ?? false;
	}

	/**
	 * Get task type for a context
	 */
	getTaskType(requestId: string): string | undefined {
		const response = this.responses[requestId];
		return response?.task_type;
	}

	/**
	 * Clear cached response
	 */
	clear(requestId: string): void {
		delete this.responses[requestId];
		delete this.loading[requestId];
		delete this.errors[requestId];
	}

	/**
	 * Clear all cached responses
	 */
	clearAll(): void {
		this.responses = {};
		this.loading = {};
		this.errors = {};
	}
}

export const contextStore = new ContextStore();
