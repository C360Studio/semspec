import type { PageLoad } from './$types';

/**
 * SSR disabled so Playwright can assert without racing SSR. This harness
 * exists only for the bug #7.10 truth-test: it mounts a TrajectoryEntryCard
 * against hard-coded fixture entries so the test doesn't need a live
 * backend or SSE seeding.
 */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { scenario: url.searchParams.get('scenario') ?? 'tool-call' };
};
