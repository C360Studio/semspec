<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import TrajectoryPanel from '$lib/components/trajectory/TrajectoryPanel.svelte';
	import GraphDetail from '$lib/components/graph/GraphDetail.svelte';
	import { ReviewDashboard } from '$lib/components/review';
	import { graphStore } from '$lib/stores/graphStore.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Loop } from '$lib/types';

	type RightTab = 'trajectory' | 'reviews' | 'agents' | 'files';

	interface Props {
		plan?: PlanWithStatus | null;
		loops?: Loop[];
	}

	let { plan = null, loops = [] }: Props = $props();

	// Graph mode: show GraphDetail instead of normal tabs
	const isGraphMode = $derived(graphStore.graphMode);
	const selectedGraphEntity = $derived(graphStore.selectedEntity);

	let selectedTab = $state<RightTab>('trajectory');

	// Plan is in an execution stage
	const isExecuting = $derived(
		plan !== null && ['implementing', 'executing', 'reviewing_rollup'].includes(plan.stage)
	);
	const isComplete = $derived(
		plan !== null && ['complete', 'failed'].includes(plan.stage)
	);

	// Filter loops to the active plan (by workflow_slug)
	const planLoops = $derived(
		plan ? loops.filter((l) => l.workflow_slug === plan.slug) : []
	);
	const activePlanLoops = $derived(
		planLoops.filter((l) => ['pending', 'executing'].includes(l.state))
	);
	const hasLoops = $derived(activePlanLoops.length > 0);

	// Auto-select most recent active or completed loop for this plan
	const effectiveLoopId = $derived(
		hasLoops ? activePlanLoops[0].loop_id : planLoops.length > 0 ? planLoops[0].loop_id : null
	);

	// Show Trajectory when executing, has loops, or is complete with history
	const showTrajectory = $derived(isExecuting || isComplete || planLoops.length > 0);

	const tabs = $derived.by(() => {
		const t: { id: RightTab; label: string; icon: string }[] = [];

		if (showTrajectory) {
			t.push({ id: 'trajectory', label: 'Trajectory', icon: 'git-branch' });
		}

		t.push({ id: 'reviews', label: 'Reviews', icon: 'check-square' });

		if (hasLoops) {
			t.push({ id: 'agents', label: 'Agents', icon: 'users' });
		}

		t.push({ id: 'files', label: 'Files', icon: 'folder' });

		return t;
	});

	// Auto-switch to Trajectory when execution starts
	const activeTab = $derived.by(() => {
		// If user manually selected a valid tab, respect it
		if (tabs.find((t) => t.id === selectedTab)) {
			// But auto-switch to trajectory when execution starts and we're on reviews
			if (isExecuting && selectedTab === 'reviews' && showTrajectory) {
				return 'trajectory';
			}
			return selectedTab;
		}
		return tabs[0]?.id ?? 'reviews';
	});
</script>

<div class="right-panel">
	{#if isGraphMode}
		<div class="tab-bar" aria-label="Graph entity context">
			<span class="tab active" aria-current="true">
				<Icon name="git-merge" size={12} />
				<span>Entity</span>
			</span>
		</div>
		<div class="tab-content" role="region" aria-label="Selected entity">
			<GraphDetail
				entity={selectedGraphEntity ?? null}
				onEntitySelect={(id) => {
					graphStore.selectEntity(id);
				}}
			/>
		</div>
	{:else}
		{#if tabs.length > 1}
			<div class="tab-bar" role="tablist" aria-label="Context panel tabs">
				{#each tabs as tab}
					<button
						class="tab"
						class:active={activeTab === tab.id}
						role="tab"
						aria-selected={activeTab === tab.id}
						onclick={() => (selectedTab = tab.id)}
					>
						<Icon name={tab.icon} size={12} />
						<span>{tab.label}</span>
					</button>
				{/each}
			</div>
		{/if}

		<div class="tab-content">
			{#if activeTab === 'trajectory'}
				{#if effectiveLoopId}
					<TrajectoryPanel loopId={effectiveLoopId} compact />
				{:else}
					<div class="empty-tab">
						<Icon name="git-branch" size={24} />
						<span>{isExecuting ? 'Waiting for agent activity...' : 'No trajectory data yet'}</span>
					</div>
				{/if}
			{:else if activeTab === 'reviews' && plan}
				<div class="review-wrapper">
					<ReviewDashboard slug={plan.slug} />
				</div>
			{:else if activeTab === 'agents' && plan}
				<div class="agents-wrapper">
					<div class="empty-tab">
						<Icon name="users" size={24} />
						<span>{activePlanLoops.length} agent{activePlanLoops.length !== 1 ? 's' : ''} active</span>
					</div>
				</div>
			{:else if activeTab === 'files'}
				<div class="empty-tab">
					<Icon name="folder" size={24} />
					<span>File viewer coming soon</span>
				</div>
			{:else}
				<div class="empty-tab">
					<Icon name="layout-grid" size={24} />
					<span>Select a plan to see details</span>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.right-panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
		background: var(--color-bg-secondary);
	}

	.tab-bar {
		display: flex;
		border-bottom: 1px solid var(--color-border);
		padding: 0 var(--space-2);
		overflow-x: auto;
	}

	.tab {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-2);
		font-size: var(--font-size-xs);
		border: none;
		background: none;
		color: var(--color-text-muted);
		cursor: pointer;
		white-space: nowrap;
		border-bottom: 2px solid transparent;
		transition: all var(--transition-fast);
	}

	.tab:hover {
		color: var(--color-text-primary);
	}

	.tab.active {
		color: var(--color-accent);
		border-bottom-color: var(--color-accent);
	}

	.tab-content {
		flex: 1;
		overflow-y: auto;
	}

	.review-wrapper,
	.agents-wrapper {
		padding: var(--space-3);
	}

	.empty-tab {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		height: 100%;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}
</style>
