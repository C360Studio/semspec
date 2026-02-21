<script lang="ts">
	import { onMount } from 'svelte';
	import { setupStore } from '$lib/stores/setup.svelte';
	import SetupWizard from '$lib/components/setup/SetupWizard.svelte';
	import ActivityFeed from '$lib/components/activity/ActivityFeed.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import ChatDropZone from '$lib/components/chat/ChatDropZone.svelte';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import LoopCard from '$lib/components/loops/LoopCard.svelte';
	import { AgentTimeline } from '$lib/components/timeline';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { projectStore } from '$lib/stores/project.svelte';
	import { browser } from '$app/environment';

	type ViewMode = 'feed' | 'timeline';
	let viewMode = $state<ViewMode>('feed');
	let mounted = $state(false);
	let dismissedNudge = $state(false);

	// Show first-plan nudge when wizard completes but no plans exist yet
	const showFirstPlanNudge = $derived(
		!dismissedNudge &&
			setupStore.step === 'complete' &&
			setupStore.isInitialized &&
			plansStore.all.length === 0
	);

	function setViewMode(mode: ViewMode) {
		viewMode = mode;
	}

	onMount(() => {
		mounted = true;
		plansStore.fetch();
		// Check if this project needs initialization
		setupStore.checkStatus();
	});

	const activeLoops = $derived(loopsStore.active);
	const pausedLoops = $derived(loopsStore.paused);

	// Determine whether to show the wizard (allow-list of active wizard steps)
	// Using allow-list ensures new WizardStep values require explicit handling
	const showWizard = $derived(
		setupStore.step === 'scaffold' ||
			setupStore.step === 'scaffolding' ||
			setupStore.step === 'detection' ||
			setupStore.step === 'checklist' ||
			setupStore.step === 'standards' ||
			setupStore.step === 'error' ||
			setupStore.step === 'initializing'
	);

	// Show a loading overlay while we check status initially
	const showInitialLoading = $derived(
		setupStore.step === 'loading' || setupStore.step === 'detecting'
	);

	// Combine all loops for timeline
	const allLoopsForTimeline = $derived(
		[...loopsStore.all].map((loop) => {
			for (const plan of plansStore.all) {
				const activeLoop = plan.active_loops?.find((l) => l.loop_id === loop.loop_id);
				if (activeLoop) {
					return { ...loop, role: activeLoop.role };
				}
			}
			return loop;
		})
	);

	function getPlanForLoop(loopId: string) {
		for (const plan of plansStore.all) {
			const loop = plan.active_loops?.find((l) => l.loop_id === loopId);
			if (loop) {
				return { plan, loop };
			}
		}
		return null;
	}

	async function handlePause(loopId: string) {
		await loopsStore.sendSignal(loopId, 'pause');
		await loopsStore.fetch();
	}

	async function handleResume(loopId: string) {
		await loopsStore.sendSignal(loopId, 'resume');
		await loopsStore.fetch();
	}

	async function handleCancel(loopId: string) {
		await loopsStore.sendSignal(loopId, 'cancel');
		await loopsStore.fetch();
	}
</script>

<svelte:head>
	<title>Activity - Semspec</title>
</svelte:head>

<!-- Setup wizard takes over the full viewport when the project is not initialized -->
{#if showWizard}
	<SetupWizard />
{:else if showInitialLoading}
	<!-- Brief loading state while checking project status -->
	<div class="init-loading" role="status" aria-live="polite">
		<Icon name="loader" size={24} class="spin" />
		<span>Loading...</span>
	</div>
{:else}
	<!-- Normal activity view once the project is initialized -->
	<div class="activity-view">
		<div class="activity-left">
			<div class="view-toggle">
				{#key mounted}
					<button
						class="toggle-btn"
						class:active={viewMode === 'feed'}
						onclick={() => setViewMode('feed')}
						type="button"
					>
						<Icon name="list" size={14} />
						Feed
					</button>
					<button
						class="toggle-btn"
						class:active={viewMode === 'timeline'}
						onclick={() => setViewMode('timeline')}
						type="button"
					>
						<Icon name="activity" size={14} />
						Timeline
					</button>
				{/key}
			</div>

			{#if viewMode === 'feed'}
				<div class="feed-section">
					<ActivityFeed />
				</div>
			{:else}
				<div class="timeline-section">
					<AgentTimeline loops={allLoopsForTimeline} showLegend={true} />
				</div>
			{/if}

			<div class="loops-section">
				<div class="loops-header">
					<Icon name="activity" size={16} />
					<span>Active Loops</span>
					<span class="loops-count">{activeLoops.length}</span>
				</div>

				{#if activeLoops.length === 0 && pausedLoops.length === 0}
					<div class="loops-empty">
						<p>No active loops</p>
					</div>
				{:else}
					<div class="loops-list">
						{#each activeLoops as loop (loop.loop_id)}
							{@const info = getPlanForLoop(loop.loop_id)}
							<LoopCard
								{loop}
								planSlug={info?.plan.slug}
								onPause={() => handlePause(loop.loop_id)}
								onResume={() => handleResume(loop.loop_id)}
								onCancel={() => handleCancel(loop.loop_id)}
							/>
						{/each}

						{#if pausedLoops.length > 0}
							<div class="loops-divider">Paused ({pausedLoops.length})</div>
							{#each pausedLoops as loop (loop.loop_id)}
								<LoopCard
									{loop}
									onResume={() => handleResume(loop.loop_id)}
									onCancel={() => handleCancel(loop.loop_id)}
								/>
							{/each}
						{/if}
					</div>
				{/if}
			</div>
		</div>

		<div class="activity-right">
			<div class="questions-section">
				<QuestionQueue />
			</div>

			<div class="chat-section">
				{#if showFirstPlanNudge}
					<div class="first-plan-nudge">
						<Icon name="lightbulb" size={16} />
						<div class="nudge-content">
							<span class="nudge-title">Ready to plan!</span>
							<span class="nudge-example">Try: <code>/plan Add user authentication</code></span>
						</div>
						<button class="dismiss-btn" onclick={() => (dismissedNudge = true)} aria-label="Dismiss">
							<Icon name="x" size={14} />
						</button>
					</div>
				{/if}
				<ChatDropZone projectId={projectStore.currentProjectId}>
					<ChatPanel />
				</ChatDropZone>
			</div>
		</div>
	</div>
{/if}

<style>
	.init-loading {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		height: 100%;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.activity-view {
		display: grid;
		grid-template-columns: 1fr 1fr;
		height: 100%;
		gap: 1px;
		background: var(--color-border);
	}

	.activity-left,
	.activity-right {
		background: var(--color-bg-primary);
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.view-toggle {
		display: flex;
		gap: var(--space-1);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.toggle-btn.active {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.feed-section {
		flex: 1;
		padding: var(--space-4);
		overflow: hidden;
		min-height: 0;
	}

	.timeline-section {
		flex: 1;
		padding: var(--space-4);
		overflow-y: auto;
		min-height: 0;
	}

	.loops-section {
		flex-shrink: 0;
		border-top: 1px solid var(--color-border);
		max-height: 200px;
		overflow-y: auto;
	}

	.loops-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border-bottom: 1px solid var(--color-border);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		position: sticky;
		top: 0;
	}

	.loops-count {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
	}

	.loops-empty {
		padding: var(--space-4);
		text-align: center;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.loops-empty p {
		margin: 0;
	}

	.loops-list {
		padding: var(--space-2);
	}

	.loops-divider {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.loops-divider::before,
	.loops-divider::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--color-border);
	}

	.questions-section {
		flex-shrink: 0;
		padding: var(--space-4);
		padding-bottom: 0;
	}

	.chat-section {
		flex: 1;
		padding: var(--space-4);
		overflow: hidden;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}

	.first-plan-nudge {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--color-accent-muted);
		border: 1px solid var(--color-accent);
		border-radius: var(--radius-md);
		margin-bottom: var(--space-3);
		flex-shrink: 0;
	}

	.nudge-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.nudge-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.nudge-example {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.nudge-example code {
		background: var(--color-bg-tertiary);
		padding: 1px 4px;
		border-radius: var(--radius-sm);
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.dismiss-btn {
		padding: var(--space-1);
		background: none;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
	}

	.dismiss-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
</style>
