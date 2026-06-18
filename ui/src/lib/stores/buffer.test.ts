import { describe, expect, it } from 'vitest';
import { appendBounded, clampEventLimit } from './buffer';

describe('bounded event buffers', () => {
	it('clamps invalid persisted limits to a safe default', () => {
		expect(clampEventLimit('not-a-number')).toBe(100);
		expect(clampEventLimit(-5)).toBe(1);
		expect(clampEventLimit(5000)).toBe(1000);
	});

	it('appends without throwing when the limit is invalid', () => {
		expect(appendBounded([1, 2, 3], 4, Number.NaN)).toEqual([1, 2, 3, 4]);
	});

	it('keeps only the most recent events within the cap', () => {
		expect(appendBounded([1, 2, 3], 4, 2)).toEqual([3, 4]);
	});
});
