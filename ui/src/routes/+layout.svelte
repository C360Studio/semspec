<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import Sidebar from '$lib/components/shared/Sidebar.svelte';
	import Header from '$lib/components/shared/Header.svelte';
	import ChatDrawer from '$lib/components/chat/ChatDrawer.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { settingsStore } from '$lib/stores/settings.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import '../app.css';

	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();

	/**
	 * Global keyboard shortcuts.
	 */
	function handleKeydown(e: KeyboardEvent): void {
		// Cmd+K (Mac) or Ctrl+K (Windows/Linux) - Toggle chat drawer
		if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
			e.preventDefault();
			chatDrawerStore.toggle();
		}
	}

	// Mark hydration complete for e2e tests
	onMount(() => {
		document.body.classList.add('hydrated');
	});

	// Apply reduced motion setting
	$effect(() => {
		if (settingsStore.reducedMotion) {
			document.documentElement.classList.add('reduced-motion');
		} else {
			document.documentElement.classList.remove('reduced-motion');
		}
	});

	// Initialize connections on mount
	$effect(() => {
		activityStore.connect();
		questionsStore.connect();
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
			questionsStore.disconnect();
			unsubscribe();
			clearInterval(interval);
		};
	});
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="app-layout">
	<Sidebar currentPath={$page.url.pathname} />

	<div class="main-area">
		<Header />

		<main class="content">
			{@render children()}
		</main>
	</div>
</div>

<!-- Global ChatDrawer -->
<ChatDrawer />

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
