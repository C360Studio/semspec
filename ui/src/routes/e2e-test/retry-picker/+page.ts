import type { PageLoad } from './$types';

/**
 * SSR disabled so Playwright stubs intercept every fetch the picker makes
 * (GET /plans/{slug}/branches and POST /plans/{slug}/retry).
 */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { slug: url.searchParams.get('slug') ?? 'retry-fixture' };
};
