<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import ActivityFeed from '$lib/components/activity/ActivityFeed.svelte';
	import PlansList from './PlansList.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	type PanelMode = 'feed' | 'plans';

	interface Props {
		plans: PlanWithStatus[];
		activeLoopCount: number;
	}

	let { plans, activeLoopCount }: Props = $props();

	let mode = $state<PanelMode>('plans');
	let manualOverride = $state(false);

	// Auto-switch: feed when there's activity from any source — active loops,
	// plan-scoped feed events, OR global loop ticks. The global branch matters
	// on /board where no plan is selected and feedStore never populates
	// (ActivityFeed renders activityStore in that case — see scope="global").
	$effect(() => {
		const hasActivity =
			activeLoopCount > 0 ||
			feedStore.events.length > 0 ||
			activityStore.recent.length > 0;
		if (hasActivity && !manualOverride) {
			mode = 'feed';
		} else if (!hasActivity) {
			mode = 'plans';
			manualOverride = false;
		}
	});

	function setMode(m: PanelMode) {
		mode = m;
		manualOverride = true;
	}

	const pendingQuestions = $derived(questionsStore.pending);
</script>

<div class="left-panel">
	<div class="panel-header">
		<div class="mode-switcher" role="radiogroup" aria-label="Left panel mode">
			<button
				class="mode-btn"
				class:active={mode === 'plans'}
				role="radio"
				aria-checked={mode === 'plans'}
				onclick={() => setMode('plans')}
			>
				<Icon name="git-pull-request" size={14} />
				<span>Plans</span>
			</button>
			<button
				class="mode-btn"
				class:active={mode === 'feed'}
				role="radio"
				aria-checked={mode === 'feed'}
				onclick={() => setMode('feed')}
			>
				<Icon name="activity" size={14} />
				<span>Feed</span>
			</button>
		</div>
	</div>

	{#if pendingQuestions.length > 0}
		<div class="questions-banner" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{pendingQuestions.length} question{pendingQuestions.length !== 1 ? 's' : ''} waiting</span>
		</div>
	{/if}

	<div class="panel-content">
		{#if mode === 'plans'}
			<PlansList {plans} />
		{:else}
			<ActivityFeed maxEvents={100} scope="global" />
		{/if}
	</div>
</div>

<style>
	.left-panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
		background: var(--color-bg-secondary);
	}

	.panel-header {
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.mode-switcher {
		display: flex;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		padding: 2px;
	}

	.mode-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex: 1;
		justify-content: center;
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		border: none;
		background: none;
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.mode-btn:hover {
		color: var(--color-text-primary);
	}

	.mode-btn.active {
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
	}

	.questions-banner {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-warning-muted, rgba(234, 179, 8, 0.1));
		color: var(--color-warning, #eab308);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border-bottom: 1px solid var(--color-border);
	}

	.panel-content {
		flex: 1;
		overflow: hidden;
	}
</style>
