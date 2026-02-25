/**
 * Mock LLM client for E2E test assertions.
 *
 * Provides access to the mock-llm server's stats and captured requests
 * for verifying that the backend is making expected LLM calls.
 *
 * Default port 11534 is used to avoid collision with backend E2E tests (11434).
 */

export interface MockLLMStats {
	total_calls: number;
	calls_by_model: Record<string, number>;
}

export interface CapturedMessage {
	role: string;
	content: string;
}

export interface CapturedRequest {
	model: string;
	messages: CapturedMessage[];
	call_index: number;
	timestamp: number;
}

export interface MockLLMRequests {
	requests_by_model: Record<string, CapturedRequest[]>;
}

export class MockLLMClient {
	constructor(private baseUrl = 'http://localhost:11534') {}

	/**
	 * Get call statistics from the mock LLM server.
	 *
	 * Returns total calls and per-model breakdown.
	 */
	async getStats(): Promise<MockLLMStats> {
		const res = await fetch(`${this.baseUrl}/stats`);
		if (!res.ok) {
			throw new Error(`Failed to get mock LLM stats: ${res.status} ${res.statusText}`);
		}
		return res.json();
	}

	/**
	 * Get captured requests from the mock LLM server.
	 *
	 * @param model - Optional model name to filter by
	 * @param callIndex - Optional 1-indexed call number to filter by
	 */
	async getRequests(model?: string, callIndex?: number): Promise<MockLLMRequests> {
		const params = new URLSearchParams();
		if (model) {
			params.set('model', model);
		}
		if (callIndex !== undefined) {
			params.set('call', String(callIndex));
		}

		const url = params.toString()
			? `${this.baseUrl}/requests?${params}`
			: `${this.baseUrl}/requests`;

		const res = await fetch(url);
		if (!res.ok) {
			throw new Error(`Failed to get mock LLM requests: ${res.status} ${res.statusText}`);
		}
		return res.json();
	}

	/**
	 * Check if the mock LLM server is healthy.
	 */
	async isHealthy(): Promise<boolean> {
		try {
			const res = await fetch(`${this.baseUrl}/health`);
			return res.ok;
		} catch {
			return false;
		}
	}

	/**
	 * Wait for the mock LLM server to become healthy.
	 *
	 * @param timeout - Maximum time to wait in milliseconds
	 * @param interval - Time between health checks in milliseconds
	 */
	async waitForHealthy(timeout = 30000, interval = 500): Promise<void> {
		const start = Date.now();
		while (Date.now() - start < timeout) {
			if (await this.isHealthy()) {
				return;
			}
			await new Promise(resolve => setTimeout(resolve, interval));
		}
		throw new Error(`Mock LLM server did not become healthy within ${timeout}ms`);
	}

	/**
	 * Assert that a specific model was called a certain number of times.
	 */
	async expectModelCalls(model: string, expectedCalls: number): Promise<void> {
		const stats = await this.getStats();
		const actualCalls = stats.calls_by_model[model] || 0;
		if (actualCalls !== expectedCalls) {
			throw new Error(
				`Expected ${expectedCalls} calls to model "${model}", but got ${actualCalls}. ` +
				`All calls: ${JSON.stringify(stats.calls_by_model)}`
			);
		}
	}

	/**
	 * Assert that total LLM calls match expected count.
	 */
	async expectTotalCalls(expectedCalls: number): Promise<void> {
		const stats = await this.getStats();
		if (stats.total_calls !== expectedCalls) {
			throw new Error(
				`Expected ${expectedCalls} total LLM calls, but got ${stats.total_calls}. ` +
				`Breakdown: ${JSON.stringify(stats.calls_by_model)}`
			);
		}
	}

	/**
	 * Get the last request made to a specific model.
	 */
	async getLastRequest(model: string): Promise<CapturedRequest | null> {
		const { requests_by_model } = await this.getRequests(model);
		const modelRequests = requests_by_model[model];
		if (!modelRequests || modelRequests.length === 0) {
			return null;
		}
		return modelRequests[modelRequests.length - 1];
	}
}
