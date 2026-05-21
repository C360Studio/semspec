import type { PageLoad } from './$types';

/**
 * SSR disabled — same pattern as other /e2e-test/* harnesses. Lets the
 * spec assert post-hydration without racing the server render.
 */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { scenario: url.searchParams.get('scenario') ?? 'empty' };
};
