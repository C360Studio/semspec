<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import Sidebar from '$lib/components/shared/Sidebar.svelte';
	import Header from '$lib/components/shared/Header.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import '../app.css';

	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();

	// Mark hydration complete for e2e tests
	onMount(() => {
		document.body.classList.add('hydrated');
	});

	// Initialize connections on mount
	$effect(() => {
		activityStore.connect();
		loopsStore.fetch();
		systemStore.fetch();
		plansStore.fetch();

		// Subscribe to activity events for chat responses
		const unsubscribe = activityStore.onEvent((event) => {
			console.log('[layout] activity event received:', event.type);
			messagesStore.handleActivityEvent(event);
		});

		// Periodic refresh for non-SSE data
		const interval = setInterval(() => {
			loopsStore.fetch();
			systemStore.fetch();
			plansStore.fetch();
		}, 30000);

		return () => {
			activityStore.disconnect();
			unsubscribe();
			clearInterval(interval);
		};
	});
</script>

<div class="app-layout">
	<Sidebar currentPath={$page.url.pathname} />

	<div class="main-area">
		<Header />

		<main class="content">
			{@render children()}
		</main>
	</div>
</div>

<style>
	.app-layout {
		display: flex;
		height: 100vh;
		overflow: hidden;
	}

	.main-area {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.content {
		flex: 1;
		overflow: auto;
	}
</style>
