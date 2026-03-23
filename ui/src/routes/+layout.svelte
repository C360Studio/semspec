<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { invalidate } from '$app/navigation';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import Header from '$lib/components/shared/Header.svelte';
	import LeftPanel from '$lib/components/shell/LeftPanel.svelte';
	import RightPanel from '$lib/components/shell/RightPanel.svelte';
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

	// Derive active plan directly from route params — no store needed
	const activePlan = $derived.by(() => {
		const slug = page.params?.slug;
		if (!slug) return null;
		return (data.plans ?? []).find((p) => p.slug === slug) ?? null;
	});

	const hasRightContext = $derived(activePlan !== null || activeLoopCount > 0);

	const configWarning = $derived(
		setupStore.step === 'scaffold' ||
			setupStore.step === 'detection' ||
			setupStore.step === 'error'
	);

	// One-time browser-only DOM setup
	onMount(() => {
		document.body.classList.add('hydrated');
		setupStore.checkStatus();
	});

	// SSE connections + polling — $effect handles cleanup via return
	$effect(() => {
		if (typeof window === 'undefined') return;

		activityStore.connect();
		questionsStore.connect();

		const unsubscribe = activityStore.onEvent((event) => {
			messagesStore.handleActivityEvent(event);
		});

		const interval = setInterval(() => {
			invalidate('app:plans');
			invalidate('app:loops');
			invalidate('app:system');
		}, 30000);

		return () => {
			activityStore.disconnect();
			questionsStore.disconnect();
			unsubscribe();
			clearInterval(interval);
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
		</div>
	{/if}

	<div class="shell-body">
		<ThreePanelLayout
			id="app-shell"
			leftOpen={true}
			rightOpen={hasRightContext}
			leftWidth={260}
			rightWidth={320}
		>
			{#snippet leftPanel()}
				<LeftPanel plans={data.plans ?? []} {activeLoopCount} />
			{/snippet}
			{#snippet centerPanel()}
				<main class="content">
					{@render children()}
				</main>
			{/snippet}
			{#snippet rightPanel()}
				<RightPanel plan={activePlan} />
			{/snippet}
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
</style>
