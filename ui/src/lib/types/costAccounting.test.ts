import { describe, expect, it } from 'vitest';
import {
	calculateCostAccounting,
	formatCostLabel,
	formatRateSourceLabel,
	measureSummaryUsage,
	measureTrajectoryUsage,
	mergeMeasuredUsage
} from './costAccounting';

describe('cost accounting', () => {
	it('measures trajectory tokens by provider and model', () => {
		const usage = measureTrajectoryUsage([
			{
				step_type: 'model_call',
				provider: 'anthropic',
				model: 'claude-sonnet',
				tokens_in: 1200,
				tokens_out: 300
			},
			{
				step_type: 'tool_call',
				tokens_in: 999,
				tokens_out: 999
			},
			{
				step_type: 'model_call',
				provider: 'anthropic',
				model: 'claude-sonnet',
				tokens_in: 800,
				tokens_out: 200
			}
		]);

		expect(usage.inputTokens).toBe(2999);
		expect(usage.outputTokens).toBe(1499);
		expect(usage.modelCalls).toBe(2);
		expect(usage.byModel).toEqual([
			expect.objectContaining({
				key: 'anthropic/claude-sonnet',
				inputTokens: 2000,
				outputTokens: 500,
				modelCalls: 2
			}),
			expect.objectContaining({
				key: 'unknown-model',
				inputTokens: 999,
				outputTokens: 999,
				modelCalls: 0
			})
		]);
	});

	it('uses summary token totals when full trajectory entries are not loaded', () => {
		const usage = measureSummaryUsage({
			model: 'qwen/qwen3.6-27b',
			tokens_in: 10_000,
			tokens_out: 2_000
		});

		expect(usage.totalTokens).toBe(12_000);
		expect(usage.byModel[0]).toMatchObject({
			model: 'qwen/qwen3.6-27b',
			inputTokens: 10_000,
			outputTokens: 2_000
		});
	});

	it('labels measured usage as unavailable when provider rates are not configured', () => {
		const usage = measureTrajectoryUsage([
			{
				step_type: 'model_call',
				provider: 'openrouter',
				model: 'qwen/qwen3.6-27b',
				tokens_in: 15_000,
				tokens_out: 4_000
			}
		]);

		const accounting = calculateCostAccounting(usage);

		expect(accounting.costUsd).toBeNull();
		expect(accounting.estimated).toBe(false);
		expect(accounting.rateSource).toMatchObject({
			kind: 'unknown',
			reason: 'provider_rate_unconfigured'
		});
		expect(formatCostLabel(accounting)).toBe('Cost unavailable');
		expect(formatCostLabel(accounting, true)).toBe('cost n/a');
		expect(formatRateSourceLabel(accounting)).toContain('provider pricing is not configured');
	});

	it('calculates estimated cost from configured provider rates', () => {
		const usage = measureTrajectoryUsage([
			{
				step_type: 'model_call',
				provider: 'anthropic',
				model: 'claude-sonnet',
				tokens_in: 1_000_000,
				tokens_out: 500_000
			}
		]);

		const accounting = calculateCostAccounting(usage, [
			{
				provider: 'anthropic',
				model: 'claude-sonnet',
				inputUsdPerMillionTokens: 3,
				outputUsdPerMillionTokens: 15,
				source: 'model-registry.test',
				sourceTimestamp: '2026-06-16T00:00:00Z'
			}
		]);

		expect(accounting.costUsd).toBe(10.5);
		expect(accounting.estimated).toBe(true);
		expect(accounting.rateSource).toMatchObject({
			kind: 'configured',
			label: 'model-registry.test',
			timestamp: '2026-06-16T00:00:00Z'
		});
		expect(formatCostLabel(accounting)).toBe('$10.50 est.');
		expect(formatRateSourceLabel(accounting)).toContain('2026-06-16T00:00:00Z');
	});

	it('marks cost as partial when one measured model has no configured rate', () => {
		const usage = mergeMeasuredUsage([
			measureSummaryUsage({ provider: 'anthropic', model: 'claude-sonnet', tokens_in: 1000, tokens_out: 100 }),
			measureSummaryUsage({ provider: 'gemini', model: 'gemini-pro', tokens_in: 2000, tokens_out: 200 })
		]);

		const accounting = calculateCostAccounting(usage, [
			{
				provider: 'anthropic',
				model: 'claude-sonnet',
				inputUsdPerMillionTokens: 3,
				outputUsdPerMillionTokens: 15,
				source: 'model-registry.test'
			}
		]);

		expect(accounting.costUsd).toBeCloseTo(0.0045);
		expect(accounting.rateSource).toMatchObject({
			kind: 'partial',
			missingModels: ['gemini/gemini-pro']
		});
		expect(formatCostLabel(accounting)).toBe('$0.0045 partial');
	});
});
