import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async () => {
	// Redirect root to board view - Board is the primary entry point
	redirect(302, '/board');
};
