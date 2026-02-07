<script lang="ts">
	import { page } from '$app/stores';
	import Sidebar from '$lib/components/shared/Sidebar.svelte';
	import Header from '$lib/components/shared/Header.svelte';
	import LoopPanel from '$lib/components/loops/LoopPanel.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';
	import '../app.css';

	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();

	// Initialize connections on mount
	$effect(() => {
		activityStore.connect();
		loopsStore.fetch();
		systemStore.fetch();

		// Periodic refresh for non-SSE data
		const interval = setInterval(() => {
			loopsStore.fetch();
			systemStore.fetch();
		}, 30000);

		return () => {
			activityStore.disconnect();
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

	<LoopPanel />
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
