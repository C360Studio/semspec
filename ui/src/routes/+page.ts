import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	// Redirect root to board view — Board is the primary entry point
	redirect(302, '/board');
};
