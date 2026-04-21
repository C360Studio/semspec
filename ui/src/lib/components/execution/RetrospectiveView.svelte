<script lang="ts">
	/**
	 * RetrospectiveView — Retrospective phases accordion view.
	 *
	 * Groups completed work by Requirement → Scenario → Task.
	 * Each level is expandable. Shows completion times and summary stats.
	 */

	import { untrack } from 'svelte';
	import Icon from '../shared/Icon.svelte';
	import type { RetrospectivePhase } from '$lib/types/execution';
	import { computeRetrospectiveStats } from '$lib/types/execution';

	interface Props {
		phases: RetrospectivePhase[];
	}

	let { phases }: Props = $props();

	// untrack: one-shot initialization from prop — intentionally not reactive to later phase changes.
	let expandedRequirements = $state<Set<string>>(
		untrack(() => phases.length > 0 ? new Set([phases[0].requirementId]) : new Set())
	);
	let expandedScenarios = $state<Set<string>>(new Set());

	function toggleRequirement(id: string) {
		const next = new Set(expandedRequirements);
		if (next.has(id)) {
			next.delete(id);
		} else {
			next.add(id);
		}
		expandedRequirements = next;
	}

	function toggleScenario(id: string) {
		const next = new Set(expandedScenarios);
		if (next.has(id)) {
			next.delete(id);
		} else {
			next.add(id);
		}
		expandedScenarios = next;
	}

	const stats = $derived(computeRetrospectiveStats(phases));

	function formatTimestamp(ts?: string): string {
		if (!ts) return '';
		return new Date(ts).toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function truncatePrompt(prompt: string, maxLen = 120): string {
		return prompt.length > maxLen ? prompt.slice(0, maxLen) + '...' : prompt;
	}
</script>

<div class="retro-view">
	<!-- Summary stats bar -->
	<div class="stats-bar" aria-label="Retrospective summary">
		<div class="stat-item">
			<span class="stat-value">{stats.totalRequirements}</span>
			<span class="stat-label">Requirements</span>
		</div>
		<div class="stat-divider" aria-hidden="true"></div>
		<div class="stat-item">
			<span class="stat-value">{stats.totalScenarios}</span>
			<span class="stat-label">Scenarios</span>
		</div>
		<div class="stat-divider" aria-hidden="true"></div>
		<div class="stat-item">
			<span class="stat-value">{stats.totalTasks}</span>
			<span class="stat-label">Tasks Completed</span>
		</div>
	</div>

	{#if phases.length === 0}
		<div class="empty-state">
			<Icon name="check-circle" size={24} />
			<span>No completed work yet</span>
			<p class="empty-hint">Tasks will appear here after execution completes</p>
		</div>
	{:else}
		<div class="requirements-list" role="list" aria-label="Requirements">
			{#each phases as phase (phase.requirementId)}
				{@const reqExpanded = expandedRequirements.has(phase.requirementId)}
				{@const scenarioCount = phase.scenarios.length}
				{@const taskCount = phase.scenarios.reduce((s, sc) => s + sc.completedTasks.length, 0)}

				<div class="requirement-section" role="listitem">
					<!-- Requirement header (accordion trigger) -->
					<button
						class="requirement-header"
						onclick={() => toggleRequirement(phase.requirementId)}
						aria-expanded={reqExpanded}
						aria-controls="req-{phase.requirementId}"
					>
						<span class="expand-icon" class:rotated={reqExpanded} aria-hidden="true">
							<Icon name="chevron-right" size={14} />
						</span>
						<span class="req-indicator" aria-hidden="true">
							<Icon name="check-circle" size={14} />
						</span>
						<span class="req-title">{phase.requirementTitle}</span>
						<div class="req-meta">
							<span class="meta-badge">{scenarioCount} scenario{scenarioCount !== 1 ? 's' : ''}</span>
							<span class="meta-badge">{taskCount} task{taskCount !== 1 ? 's' : ''}</span>
						</div>
					</button>

					<!-- Requirement body -->
					{#if reqExpanded}
						<div class="requirement-body" id="req-{phase.requirementId}" role="list" aria-label="Scenarios for {phase.requirementTitle}">
							{#each phase.scenarios as scenario (scenario.scenarioId)}
								{@const scExpanded = expandedScenarios.has(scenario.scenarioId)}
								{@const taskCount2 = scenario.completedTasks.length}

								<div class="scenario-section" role="listitem">
									<!-- Scenario header -->
									<button
										class="scenario-header"
										onclick={() => toggleScenario(scenario.scenarioId)}
										aria-expanded={scExpanded}
										aria-controls="sc-{scenario.scenarioId}"
									>
										<span class="expand-icon" class:rotated={scExpanded} aria-hidden="true">
											<Icon name="chevron-right" size={12} />
										</span>
										<span class="sc-indicator" aria-hidden="true">
											<Icon name="list-checks" size={12} />
										</span>
										<span class="sc-title">{scenario.scenarioTitle}</span>
										<span class="meta-badge task-badge">{taskCount2} task{taskCount2 !== 1 ? 's' : ''}</span>
									</button>

									<!-- Scenario tasks -->
									{#if scExpanded}
										<div class="scenario-body" id="sc-{scenario.scenarioId}" role="list" aria-label="Tasks for {scenario.scenarioTitle}">
											{#if scenario.completedTasks.length === 0}
												<div class="no-tasks">No completed tasks</div>
											{:else}
												{#each scenario.completedTasks as task (task.taskId)}
													<div class="task-item" role="listitem">
														<span class="task-check" aria-hidden="true">
															<Icon name="check" size={12} />
														</span>
														<div class="task-content">
															<span class="task-prompt">{truncatePrompt(task.prompt)}</span>
															{#if task.completedAt}
																<span class="task-time">
																	<Icon name="clock" size={10} />
																	{formatTimestamp(task.completedAt)}
																</span>
															{/if}
														</div>
														<span class="task-id">{task.taskId.slice(0, 8)}</span>
													</div>
												{/each}
											{/if}
										</div>
									{/if}
								</div>
							{/each}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.retro-view {
		display: flex;
		flex-direction: column;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.stats-bar {
		display: flex;
		align-items: center;
		gap: 0;
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
	}

	.stat-item {
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 0 var(--space-4);
	}

	.stat-value {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
	}

	.stat-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.stat-divider {
		width: 1px;
		height: 32px;
		background: var(--color-border);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-8);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		margin: 0;
	}

	.requirements-list {
		display: flex;
		flex-direction: column;
	}

	.requirement-section {
		border-bottom: 1px solid var(--color-border);
	}

	.requirement-section:last-child {
		border-bottom: none;
	}

	.requirement-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-3) var(--space-4);
		background: transparent;
		border: none;
		cursor: pointer;
		text-align: left;
		transition: background-color var(--transition-fast);
	}

	.requirement-header:hover {
		background: var(--color-bg-tertiary);
	}

	.expand-icon {
		display: flex;
		align-items: center;
		color: var(--color-text-muted);
		flex-shrink: 0;
		transition: transform var(--transition-fast);
	}

	.expand-icon.rotated {
		transform: rotate(90deg);
	}

	.req-indicator {
		display: flex;
		align-items: center;
		color: var(--color-success);
		flex-shrink: 0;
	}

	.req-title {
		flex: 1;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.req-meta {
		display: flex;
		gap: var(--space-2);
		flex-shrink: 0;
	}

	.meta-badge {
		font-size: 10px;
		padding: 1px 6px;
		background: var(--color-bg-elevated);
		color: var(--color-text-muted);
		border-radius: var(--radius-full);
	}

	.requirement-body {
		background: var(--color-bg-primary);
	}

	.scenario-section {
		border-top: 1px solid var(--color-border);
	}

	.scenario-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-2) var(--space-4) var(--space-2) var(--space-6);
		background: transparent;
		border: none;
		cursor: pointer;
		text-align: left;
		transition: background-color var(--transition-fast);
	}

	.scenario-header:hover {
		background: var(--color-bg-tertiary);
	}

	.sc-indicator {
		display: flex;
		align-items: center;
		color: var(--color-accent);
		flex-shrink: 0;
	}

	.sc-title {
		flex: 1;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.task-badge {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.scenario-body {
		background: var(--color-bg-secondary);
		padding: var(--space-2) 0;
	}

	.no-tasks {
		padding: var(--space-2) var(--space-8);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-style: italic;
	}

	.task-item {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4) var(--space-2) var(--space-10);
		transition: background-color var(--transition-fast);
	}

	.task-item:hover {
		background: var(--color-bg-tertiary);
	}

	.task-check {
		display: flex;
		align-items: center;
		color: var(--color-success);
		flex-shrink: 0;
		margin-top: 2px;
	}

	.task-content {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.task-prompt {
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		line-height: 1.4;
	}

	.task-time {
		display: flex;
		align-items: center;
		gap: 3px;
		font-size: 10px;
		color: var(--color-text-muted);
	}

	.task-id {
		font-family: var(--font-family-mono);
		font-size: 10px;
		color: var(--color-text-muted);
		flex-shrink: 0;
	}
</style>
