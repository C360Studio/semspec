<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import { getTaskStatusInfo, type TaskStatus } from '$lib/types/task';
	import { getScenarioStatusInfo } from '$lib/types/scenario';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import type { KanbanCardItem } from '$lib/types/kanban';

	interface Props {
		item: KanbanCardItem | null;
	}

	let { item }: Props = $props();

	const statusInfo = $derived(
		item
			? item.type === 'task'
				? getTaskStatusInfo(item.originalStatus as TaskStatus)
				: getScenarioStatusInfo(item.originalStatus as any)
			: null
	);

	// Get scenarios for the item's requirement
	const relatedScenarios = $derived.by(() => {
		if (!item?.requirementId) return [];
		for (const scenarios of Object.values(kanbanStore.scenariosByPlan)) {
			if (!Array.isArray(scenarios)) continue;
			const matching = scenarios.filter((s) => s.requirement_id === item.requirementId);
			if (matching.length > 0) return matching;
		}
		return [];
	});
</script>

{#if item}
	<div class="detail-panel">
		<div class="panel-header">
			<div class="panel-title-row">
				<span class="status-dot status-{statusInfo?.color}"></span>
				<span class="panel-type">{item.type === 'task' ? (item.taskType ?? 'Task') : 'Scenario'}</span>
			</div>
			<span class="panel-status badge badge-{statusInfo?.color === 'blue' ? 'info' : statusInfo?.color === 'green' ? 'success' : statusInfo?.color === 'red' || statusInfo?.color === 'orange' ? 'error' : statusInfo?.color === 'yellow' ? 'warning' : 'neutral'}">
				{statusInfo?.label}
			</span>
		</div>

		<h3 class="panel-item-title">{item.title}</h3>

		<div class="panel-meta">
			<div class="meta-row">
				<Icon name="folder" size={12} />
				<a href="/plans/{item.planSlug}" class="plan-link">{item.planSlug}</a>
			</div>

			{#if item.requirementTitle}
				<div class="meta-row">
					<Icon name="target" size={12} />
					<span>{item.requirementTitle}</span>
				</div>
			{/if}
		</div>

		{#if item.agentRole}
			<div class="panel-section">
				<h4 class="section-label">Agent</h4>
				<AgentBadge
					role={item.agentRole}
					model={item.agentModel}
					state={item.agentState ?? 'idle'}
				/>
			</div>
		{/if}

		{#if item.rejection}
			<div class="panel-section">
				<h4 class="section-label">Rejection</h4>
				<div class="rejection-detail">
					<p class="rejection-reason">{item.rejection.reason}</p>
					<span class="rejection-meta">
						Type: {item.rejection.type} &middot; Iteration {item.rejection.iteration}
					</span>
				</div>
			</div>
		{/if}

		{#if item.iteration !== undefined && item.maxIterations !== undefined}
			<div class="panel-section">
				<h4 class="section-label">Progress</h4>
				<div class="progress-row">
					<span>Attempt {item.iteration} of {item.maxIterations}</span>
					<div class="progress-bar">
						<div
							class="progress-fill"
							style="width: {(item.iteration / item.maxIterations) * 100}%"
						></div>
					</div>
				</div>
			</div>
		{/if}

		{#if relatedScenarios.length > 0}
			<div class="panel-section">
				<h4 class="section-label">Scenarios ({relatedScenarios.length})</h4>
				<div class="scenario-list">
					{#each relatedScenarios as scenario (scenario.id)}
						<div class="scenario-item">
							<div class="scenario-line">
								<span class="scenario-keyword">Given</span> {scenario.given}
							</div>
							<div class="scenario-line">
								<span class="scenario-keyword">When</span> {scenario.when}
							</div>
							{#each scenario.then as thenClause}
								<div class="scenario-line">
									<span class="scenario-keyword">Then</span> {thenClause}
								</div>
							{/each}
						</div>
					{/each}
				</div>
			</div>
		{/if}

		<div class="panel-actions">
			<a href="/plans/{item.planSlug}?task={item.id}" class="action-link">
				<Icon name="external-link" size={14} />
				Open in plan detail
			</a>
			{#if item.agentRole}
				<a href="/activity" class="action-link">
					<Icon name="activity" size={14} />
					View activity
				</a>
			{/if}
		</div>
	</div>
{:else}
	<div class="empty-panel">
		<Icon name="mouse-pointer" size={32} />
		<p>Select a card to view details</p>
		<span class="shortcut-hint">Cmd+J to toggle this panel</span>
	</div>
{/if}

<style>
	.detail-panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-4);
		height: 100%;
		overflow-y: auto;
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.panel-title-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-full);
	}

	.status-gray { background: var(--color-text-muted); }
	.status-blue { background: var(--color-accent); }
	.status-green { background: var(--color-success); }
	.status-yellow { background: var(--color-warning); }
	.status-red { background: var(--color-error); }
	.status-orange { background: var(--color-warning); }

	.panel-type {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: capitalize;
	}

	.panel-status {
		font-size: var(--font-size-xs);
	}

	.panel-item-title {
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
		line-height: var(--line-height-tight);
	}

	.panel-meta {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.meta-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.plan-link {
		color: var(--color-accent);
		text-decoration: none;
	}

	.plan-link:hover {
		text-decoration: underline;
	}

	.panel-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.rejection-detail {
		background: var(--color-warning-muted);
		border-radius: var(--radius-md);
		padding: var(--space-3);
	}

	.rejection-reason {
		font-size: var(--font-size-sm);
		color: var(--color-warning);
		margin: 0 0 var(--space-2);
		line-height: var(--line-height-normal);
	}

	.rejection-meta {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.progress-row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.progress-bar {
		height: 4px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--color-accent);
		transition: width var(--transition-base);
	}

	.scenario-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.scenario-item {
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.scenario-line {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
	}

	.scenario-keyword {
		font-weight: var(--font-weight-semibold);
		color: var(--color-info);
	}

	.panel-actions {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-top: auto;
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.action-link {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		text-decoration: none;
		transition: all var(--transition-fast);
	}

	.action-link:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-accent);
		text-decoration: none;
	}

	.empty-panel {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		height: 100%;
		gap: var(--space-3);
		color: var(--color-text-muted);
		text-align: center;
		padding: var(--space-6);
	}

	.empty-panel p {
		margin: 0;
		font-size: var(--font-size-sm);
	}

	.shortcut-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		opacity: 0.6;
	}
</style>
