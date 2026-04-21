import type { PageLoad } from './$types';

/**
 * SSR disabled so Playwright page.route stubs intercept every PlanWorkspace
 * fetch. Mirrors the /status pattern.
 */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { slug: url.searchParams.get('slug') ?? '' };
};
