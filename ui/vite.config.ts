import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		host: true
		// API routing handled by Caddy gateway — no vite proxy needed
	},
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'node',
		alias: {
			'$lib': new URL('./src/lib', import.meta.url).pathname
		}
	}
});
