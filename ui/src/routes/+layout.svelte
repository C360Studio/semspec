<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import Header from '$lib/components/shared/Header.svelte';
	import LeftPanel from '$lib/components/shell/LeftPanel.svelte';
	import Toast from '$lib/components/shared/Toast.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { settingsStore } from '$lib/stores/settings.svelte';
	import { setupStore } from '$lib/stores/setup.svelte';
	import '../app.css';

	import type { Snippet } from 'svelte';
	import type { LayoutData } from './$types';

	interface Props {
		data: LayoutData;
		children: Snippet;
	}

	let { data, children }: Props = $props();

	const activeLoopCount = $derived(
		(data.loops ?? []).filter((l) => ['pending', 'executing', 'paused'].includes(l.state)).length
	);

	const configWarning = $derived(
		setupStore.step === 'scaffold' ||
			setupStore.step === 'detection' ||
			setupStore.step === 'config_required' ||
			setupStore.step === 'error'
	);

	// Hard gate: redirect to /settings when required config is missing
	$effect(() => {
		if (setupStore.step === 'config_required' && page.url.pathname !== '/settings') {
			goto('/settings');
		}
	});

	// One-time browser-only setup: hydration marker, setup probe, and
	// SSE connections. SSE belongs in onMount (not $effect) because the
	// layout is a long-lived root component and we want exactly one
	// EventSource per store for its lifetime. Putting the same code in
	// $effect reintroduces the reconnect churn previously fixed in
	// commit 0a6381e: any untracked reactive read (direct or indirect)
	// re-runs the effect, closes the EventSource, and opens a new one —
	// producing the 5-30s "context canceled" → "connected" log pattern.
	onMount(() => {
		document.body.classList.add('hydrated');
		setupStore.checkStatus();

		if (typeof window === 'undefined') return;

		activityStore.connect();
		questionsStore.connect();

		const unsubscribe = activityStore.onEvent((event) => {
			messagesStore.handleActivityEvent(event);
		});

		return () => {
			activityStore.disconnect();
			questionsStore.disconnect();
			unsubscribe();
		};
	});

	$effect(() => {
		if (typeof document === 'undefined') return;
		document.documentElement.classList.toggle('reduced-motion', settingsStore.reducedMotion);
	});
</script>

<div class="app-shell">
	<Header {activeLoopCount} />

	{#if configWarning}
		<div class="config-warning" role="alert">
			<Icon name="alert-triangle" size={16} />
			<span>Project not fully configured — some features may be limited.</span>
			<a href="/settings" class="config-warning-link">Review Settings</a>
		</div>
	{/if}

	<div class="shell-body">
		<ThreePanelLayout
			id="app-shell"
			leftOpen={true}
			hideRight={true}
			leftWidth={260}
		>
			{#snippet leftPanel()}
				<LeftPanel plans={data.plans ?? []} {activeLoopCount} />
			{/snippet}
			{#snippet centerPanel()}
				<main class="content">
					{@render children()}
				</main>
			{/snippet}
			{#snippet rightPanel()}{/snippet}
		</ThreePanelLayout>
	</div>

	<Toast />
</div>

<style>
	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}

	.app-shell {
		display: flex;
		flex-direction: column;
		height: 100vh;
		overflow: hidden;
	}

	.shell-body {
		flex: 1;
		overflow: hidden;
	}

	.content {
		height: 100%;
		overflow: auto;
	}

	.config-warning {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-warning-muted);
		color: var(--color-warning);
		font-size: var(--font-size-xs);
		border-bottom: 1px solid var(--color-warning);
	}

	.config-warning-link {
		margin-left: auto;
		color: var(--color-warning);
		font-weight: var(--font-weight-medium);
		text-decoration: underline;
	}

	.config-warning-link:hover {
		opacity: 0.8;
	}
</style>
