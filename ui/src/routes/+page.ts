import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	// Plans and loops come from the layout — no page-level fetching needed.
	return {};
};
