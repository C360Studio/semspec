import type { HandleFetch } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';

/**
 * Rewrite relative API paths to the backend during SSR.
 *
 * In the browser, Caddy routes /workflow-api/*, /agentic-dispatch/*, etc.
 * to the Go backend. During SSR the SvelteKit node server has no such
 * routing, so we rewrite the URL to BACKEND_URL (defaults to Caddy gateway
 * on the Docker network).
 */

const BACKEND_URL = env.BACKEND_URL || 'http://gateway:8080';

const API_PREFIXES = [
	'/workflow-api',
	'/agentic-dispatch',
	'/project-api',
	'/message-logger',
	'/trajectory-api',
	'/graphql',
	'/health',
	'/readyz',
	'/metrics'
];

export const handleFetch: HandleFetch = async ({ request, fetch }) => {
	const url = new URL(request.url);

	for (const prefix of API_PREFIXES) {
		if (url.pathname.startsWith(prefix)) {
			const backendRequest = new Request(
				`${BACKEND_URL}${url.pathname}${url.search}`,
				request
			);
			return fetch(backendRequest);
		}
	}

	return fetch(request);
};
