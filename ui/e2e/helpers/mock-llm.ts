/**
 * Mock LLM client for asserting on LLM call counts and request content.
 * The mock LLM runs on port 11534 for UI E2E tests.
 */

const MOCK_LLM_BASE = 'http://localhost:11534';

export interface MockLLMStats {
	total_requests: number;
	models: Record<string, number>;
}

export interface MockLLMRequest {
	model: string;
	messages: Array<{ role: string; content: string }>;
	timestamp: string;
}

export class MockLLMClient {
	private baseUrl: string;

	constructor(baseUrl = MOCK_LLM_BASE) {
		this.baseUrl = baseUrl;
	}

	async waitForHealthy(timeoutMs = 10000): Promise<void> {
		const start = Date.now();
		while (Date.now() - start < timeoutMs) {
			try {
				const res = await fetch(`${this.baseUrl}/health`);
				if (res.ok) return;
			} catch {
				// not ready yet
			}
			await new Promise((r) => setTimeout(r, 500));
		}
		throw new Error(`Mock LLM not healthy after ${timeoutMs}ms`);
	}

	async getStats(): Promise<MockLLMStats> {
		const res = await fetch(`${this.baseUrl}/stats`);
		if (!res.ok) throw new Error(`Mock LLM stats failed (${res.status})`);
		return res.json();
	}

	async getRequests(model?: string): Promise<MockLLMRequest[]> {
		const url = model
			? `${this.baseUrl}/requests?model=${encodeURIComponent(model)}`
			: `${this.baseUrl}/requests`;
		const res = await fetch(url);
		if (!res.ok) throw new Error(`Mock LLM requests failed (${res.status})`);
		return res.json();
	}

	async resetStats(): Promise<void> {
		const res = await fetch(`${this.baseUrl}/stats/reset`, { method: 'POST' });
		if (!res.ok) throw new Error(`Mock LLM reset failed (${res.status})`);
	}

	/**
	 * Switch mock LLM to a different scenario and reset call counters.
	 * Reloads fixtures from the sibling scenario directory.
	 */
	async resetScenario(scenario: string): Promise<void> {
		const res = await fetch(
			`${this.baseUrl}/reset?scenario=${encodeURIComponent(scenario)}`,
			{ method: 'POST' }
		);
		if (!res.ok) throw new Error(`Mock LLM scenario reset failed (${res.status})`);
	}

	/**
	 * Set a per-response delay on the mock LLM. Useful for tests that need
	 * to assert mid-LLM UI state (e.g. "drafting" hints) which would otherwise
	 * be invisible because mock fixtures normally respond in <1ms. Pass 0 to
	 * clear the delay.
	 */
	async setDelay(delayMs: number): Promise<void> {
		const res = await fetch(`${this.baseUrl}/admin/config`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ delay_ms: delayMs })
		});
		if (!res.ok) throw new Error(`Mock LLM setDelay failed (${res.status})`);
	}
}
