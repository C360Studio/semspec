const DEFAULT_LIMIT = 100;
const MIN_LIMIT = 1;
const MAX_LIMIT = 1000;

export function clampEventLimit(value: unknown, fallback = DEFAULT_LIMIT): number {
	const numeric = typeof value === 'number' ? value : Number(value);
	if (!Number.isFinite(numeric)) return fallback;
	return Math.max(MIN_LIMIT, Math.min(MAX_LIMIT, Math.trunc(numeric)));
}

export function appendBounded<T>(items: T[], item: T, limit: unknown): T[] {
	const capped = clampEventLimit(limit);
	return [...items.slice(-(capped - 1)), item];
}
