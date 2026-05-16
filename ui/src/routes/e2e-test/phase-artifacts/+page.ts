import type { PageLoad } from './$types';

/**
 * SSR disabled so Playwright page.route stubs intercept every artifacts
 * fetch from PhaseArtifactsView. Mirrors the /plan-workspace pattern.
 */
export const ssr = false;

export const load: PageLoad = async ({ url }) => {
	return { slug: url.searchParams.get('slug') ?? '' };
};
