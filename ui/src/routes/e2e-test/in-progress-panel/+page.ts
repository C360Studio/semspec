import type { PageLoad } from './$types';

/** SSR disabled — Playwright harness for InProgressPanel. */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { scenario: url.searchParams.get('scenario') ?? 'drafting' };
};
