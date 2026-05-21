import type { PageLoad } from './$types';

/** SSR disabled — Playwright harness for ActivityFeed autoscroll behavior. */
export const ssr = false;

export const load: PageLoad = async () => {
	return {};
};
