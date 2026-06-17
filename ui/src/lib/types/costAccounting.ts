export type UsageEntry = {
	step_type?: string;
	model?: string | null;
	provider?: string | null;
	tokens_in?: number | null;
	tokens_out?: number | null;
};

export type UsageFallback = {
	model?: string | null;
	provider?: string | null;
	tokens_in?: number | null;
	tokens_out?: number | null;
};

export type ModelTokenUsage = {
	key: string;
	model: string;
	provider?: string;
	inputTokens: number;
	outputTokens: number;
	totalTokens: number;
	modelCalls: number;
};

export type MeasuredTokenUsage = {
	inputTokens: number;
	outputTokens: number;
	totalTokens: number;
	modelCalls: number;
	byModel: ModelTokenUsage[];
};

export type ProviderRate = {
	model: string;
	provider?: string;
	inputUsdPerMillionTokens: number;
	outputUsdPerMillionTokens: number;
	source: string;
	sourceTimestamp?: string;
};

export type CostRateSource =
	| {
		kind: 'configured';
		label: string;
		timestamp?: string;
	}
	| {
		kind: 'partial';
		label: string;
		timestamp?: string;
		missingModels: string[];
	}
	| {
		kind: 'unknown';
		label: string;
		reason: 'no_measured_usage' | 'provider_rate_unconfigured';
	};

export type CostAccounting = {
	usage: MeasuredTokenUsage;
	costUsd: number | null;
	estimated: boolean;
	rateSource: CostRateSource;
};

const UNKNOWN_MODEL = 'unknown-model';

export function emptyMeasuredUsage(): MeasuredTokenUsage {
	return {
		inputTokens: 0,
		outputTokens: 0,
		totalTokens: 0,
		modelCalls: 0,
		byModel: []
	};
}

export function measureTrajectoryUsage(
	entries: UsageEntry[],
	fallback: UsageFallback = {}
): MeasuredTokenUsage {
	const totals = new Map<string, ModelTokenUsage>();
	let inputTokens = 0;
	let outputTokens = 0;
	let modelCalls = 0;

	for (const entry of entries) {
		const input = normalizeTokenCount(entry.tokens_in);
		const output = normalizeTokenCount(entry.tokens_out);
		if (entry.step_type === 'model_call') modelCalls += 1;
		if (input === 0 && output === 0) continue;

		const model = normalizeModel(entry.model ?? fallback.model);
		const provider = normalizeOptional(entry.provider ?? fallback.provider);
		const key = usageKey(provider, model);
		const existing = totals.get(key) ?? {
			key,
			model,
			provider,
			inputTokens: 0,
			outputTokens: 0,
			totalTokens: 0,
			modelCalls: 0
		};
		existing.inputTokens += input;
		existing.outputTokens += output;
		existing.totalTokens += input + output;
		if (entry.step_type === 'model_call') existing.modelCalls += 1;
		totals.set(key, existing);
		inputTokens += input;
		outputTokens += output;
	}

	return {
		inputTokens,
		outputTokens,
		totalTokens: inputTokens + outputTokens,
		modelCalls,
		byModel: [...totals.values()].sort((a, b) => a.key.localeCompare(b.key))
	};
}

export function measureSummaryUsage(summary: UsageFallback): MeasuredTokenUsage {
	const input = normalizeTokenCount(summary.tokens_in);
	const output = normalizeTokenCount(summary.tokens_out);
	if (input === 0 && output === 0) return emptyMeasuredUsage();

	const model = normalizeModel(summary.model);
	const provider = normalizeOptional(summary.provider);
	const key = usageKey(provider, model);
	return {
		inputTokens: input,
		outputTokens: output,
		totalTokens: input + output,
		modelCalls: 0,
		byModel: [{
			key,
			model,
			provider,
			inputTokens: input,
			outputTokens: output,
			totalTokens: input + output,
			modelCalls: 0
		}]
	};
}

export function mergeMeasuredUsage(usages: MeasuredTokenUsage[]): MeasuredTokenUsage {
	const merged = new Map<string, ModelTokenUsage>();
	let inputTokens = 0;
	let outputTokens = 0;
	let modelCalls = 0;

	for (const usage of usages) {
		inputTokens += usage.inputTokens;
		outputTokens += usage.outputTokens;
		modelCalls += usage.modelCalls;
		for (const item of usage.byModel) {
			const existing = merged.get(item.key) ?? {
				key: item.key,
				model: item.model,
				provider: item.provider,
				inputTokens: 0,
				outputTokens: 0,
				totalTokens: 0,
				modelCalls: 0
			};
			existing.inputTokens += item.inputTokens;
			existing.outputTokens += item.outputTokens;
			existing.totalTokens += item.totalTokens;
			existing.modelCalls += item.modelCalls;
			merged.set(item.key, existing);
		}
	}

	return {
		inputTokens,
		outputTokens,
		totalTokens: inputTokens + outputTokens,
		modelCalls,
		byModel: [...merged.values()].sort((a, b) => a.key.localeCompare(b.key))
	};
}

export function calculateCostAccounting(
	usage: MeasuredTokenUsage,
	rates: ProviderRate[] = []
): CostAccounting {
	if (usage.totalTokens === 0) {
		return {
			usage,
			costUsd: null,
			estimated: false,
			rateSource: {
				kind: 'unknown',
				label: 'No measured token usage',
				reason: 'no_measured_usage'
			}
		};
	}

	if (rates.length === 0) {
		return unknownRateAccounting(usage);
	}

	let costUsd = 0;
	const matchedRates: ProviderRate[] = [];
	const missingModels: string[] = [];

	for (const item of usage.byModel) {
		const rate = findRateForUsage(item, rates);
		if (!rate) {
			missingModels.push(displayModel(item));
			continue;
		}
		matchedRates.push(rate);
		costUsd += (item.inputTokens / 1_000_000) * rate.inputUsdPerMillionTokens;
		costUsd += (item.outputTokens / 1_000_000) * rate.outputUsdPerMillionTokens;
	}

	if (matchedRates.length === 0) {
		return unknownRateAccounting(usage);
	}

	const sourceLabel = summarizeRateSource(matchedRates);
	const timestamp = newestTimestamp(matchedRates);
	if (missingModels.length > 0) {
		return {
			usage,
			costUsd,
			estimated: true,
			rateSource: {
				kind: 'partial',
				label: `${sourceLabel}; missing rates for ${missingModels.join(', ')}`,
				timestamp,
				missingModels
			}
		};
	}

	return {
		usage,
		costUsd,
		estimated: true,
		rateSource: {
			kind: 'configured',
			label: sourceLabel,
			timestamp
		}
	};
}

export function formatCostLabel(accounting: CostAccounting, compact = false): string {
	if (accounting.costUsd !== null) {
		const suffix = accounting.rateSource.kind === 'partial' ? ' partial' : ' est.';
		return `${formatUSD(accounting.costUsd)}${suffix}`;
	}
	if (accounting.rateSource.kind === 'unknown' && accounting.rateSource.reason === 'no_measured_usage') {
		return compact ? 'no usage' : 'No measured usage';
	}
	return compact ? 'cost n/a' : 'Cost unavailable';
}

export function formatRateSourceLabel(accounting: CostAccounting): string {
	if (accounting.rateSource.kind === 'configured') {
		return withTimestamp(`Rates: ${accounting.rateSource.label}`, accounting.rateSource.timestamp);
	}
	if (accounting.rateSource.kind === 'partial') {
		return withTimestamp(`Partial rates: ${accounting.rateSource.label}`, accounting.rateSource.timestamp);
	}
	if (accounting.rateSource.reason === 'no_measured_usage') {
		return accounting.rateSource.label;
	}
	return 'Measured tokens are available, but provider pricing is not configured.';
}

function unknownRateAccounting(usage: MeasuredTokenUsage): CostAccounting {
	return {
		usage,
		costUsd: null,
		estimated: false,
		rateSource: {
			kind: 'unknown',
			label: 'Provider pricing is not configured',
			reason: 'provider_rate_unconfigured'
		}
	};
}

function findRateForUsage(usage: ModelTokenUsage, rates: ProviderRate[]): ProviderRate | undefined {
	const model = normalizeModel(usage.model).toLowerCase();
	const provider = normalizeOptional(usage.provider)?.toLowerCase();
	return rates.find((rate) =>
		normalizeModel(rate.model).toLowerCase() === model &&
		normalizeOptional(rate.provider)?.toLowerCase() === provider
	) ?? rates.find((rate) =>
		normalizeModel(rate.model).toLowerCase() === model &&
		!normalizeOptional(rate.provider)
	);
}

function summarizeRateSource(rates: ProviderRate[]): string {
	const sources = [...new Set(rates.map((rate) => rate.source).filter(Boolean))];
	if (sources.length === 0) return 'configured provider rates';
	if (sources.length === 1) return sources[0];
	return `${sources.length} configured rate sources`;
}

function newestTimestamp(rates: ProviderRate[]): string | undefined {
	const timestamps = rates
		.map((rate) => rate.sourceTimestamp)
		.filter((ts): ts is string => Boolean(ts))
		.sort();
	return timestamps[timestamps.length - 1];
}

function withTimestamp(label: string, timestamp?: string): string {
	return timestamp ? `${label} (${timestamp})` : label;
}

function displayModel(usage: ModelTokenUsage): string {
	return usage.provider ? `${usage.provider}/${usage.model}` : usage.model;
}

function formatUSD(value: number): string {
	if (value > 0 && value < 0.0001) return '<$0.0001';
	if (value < 1) return `$${value.toFixed(4)}`;
	return `$${value.toFixed(2)}`;
}

function normalizeTokenCount(value: number | null | undefined): number {
	if (!Number.isFinite(value) || !value || value < 0) return 0;
	return Math.round(value);
}

function normalizeModel(value: string | null | undefined): string {
	const trimmed = value?.trim();
	return trimmed || UNKNOWN_MODEL;
}

function normalizeOptional(value: string | null | undefined): string | undefined {
	const trimmed = value?.trim();
	return trimmed || undefined;
}

function usageKey(provider: string | undefined, model: string): string {
	return provider ? `${provider}/${model}` : model;
}
