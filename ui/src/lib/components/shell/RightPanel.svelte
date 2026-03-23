<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import TrajectoryPanel from '$lib/components/trajectory/TrajectoryPanel.svelte';
	import { ReviewDashboard } from '$lib/components/review';
	import type { PlanWithStatus } from '$lib/types/plan';

	type RightTab = 'trajectory' | 'reviews' | 'agents' | 'files';

	interface Props {
		plan?: PlanWithStatus | null;
	}

	let { plan = null }: Props = $props();

	let selectedTab = $state<RightTab>('trajectory');

	const activeLoops = $derived(plan?.active_loops ?? []);
	const hasLoops = $derived(activeLoops.length > 0);

	// Auto-select first active loop
	const effectiveLoopId = $derived(
		hasLoops ? activeLoops[0].loop_id : null
	);

	const tabs = $derived.by(() => {
		const t: { id: RightTab; label: string; icon: string }[] = [];

		if (effectiveLoopId || hasLoops) {
			t.push({ id: 'trajectory', label: 'Trajectory', icon: 'git-branch' });
		}

		t.push({ id: 'reviews', label: 'Reviews', icon: 'check-square' });

		if (hasLoops) {
			t.push({ id: 'agents', label: 'Agents', icon: 'users' });
		}

		t.push({ id: 'files', label: 'Files', icon: 'folder' });

		return t;
	});

	// Ensure selected tab is valid for current state
	const activeTab = $derived(
		tabs.find((t) => t.id === selectedTab) ? selectedTab : tabs[0]?.id ?? 'reviews'
	);
</script>

<div class="right-panel">
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
		{#if activeTab === 'trajectory' && effectiveLoopId}
			<TrajectoryPanel loopId={effectiveLoopId} compact />
		{:else if activeTab === 'reviews' && plan}
			<div class="review-wrapper">
				<ReviewDashboard slug={plan.slug} />
			</div>
		{:else if activeTab === 'agents' && plan}
			<div class="agents-wrapper">
				<div class="empty-tab">
					<Icon name="users" size={24} />
					<span>{activeLoops.length} agent{activeLoops.length !== 1 ? 's' : ''} active</span>
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
