import { describe, expect, it } from 'vitest';
import type { PlanWithStatus } from './plan';
import {
	compactPlanText,
	isRawPromptLike,
	planDisplayTitle,
	shouldCollapsePlanText
} from './planDisplay';

function plan(overrides: Partial<PlanWithStatus>): PlanWithStatus {
	return {
		id: 'plan.demo',
		slug: 'abc123',
		stage: 'drafting',
		approved: false,
		created_at: '2026-06-18T12:00:00Z',
		active_loops: [],
		...overrides
	} as PlanWithStatus;
}

describe('plan display helpers', () => {
	it('keeps short authored titles as the display title', () => {
		expect(planDisplayTitle(plan({ title: 'MAVLink fallback support' }))).toBe(
			'MAVLink fallback support'
		);
	});

	it('does not promote raw prompts to the page title', () => {
		const rawPrompt = `Starting from the existing OpenSensorHub MAVSDK addon, design and implement MAVLink support through the Connected Systems API.

The implementation must provide full plugin coverage, tests, and documentation.`;

		expect(isRawPromptLike(rawPrompt)).toBe(true);
		expect(planDisplayTitle(plan({ title: rawPrompt, goal: rawPrompt }))).toBe('Plan abc123');
		expect(shouldCollapsePlanText(rawPrompt)).toBe(true);
	});

	it('falls back to a concise goal when title is raw but goal is clean', () => {
		expect(
			planDisplayTitle(plan({
				title: 'Implement this feature with a long operational instruction payload that should not become the title',
				goal: 'Add MAVLink fallback support'
			}))
		).toBe('Add MAVLink fallback support');
	});

	it('compacts raw source text for summary rows', () => {
		const compact = compactPlanText('a '.repeat(200), 24);
		expect(compact.length).toBeLessThanOrEqual(24);
		expect(compact.endsWith('...')).toBe(true);
	});
});
