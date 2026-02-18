<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { Plan } from '$lib/types/plan';

	interface Props {
		plan: Plan;
	}

	let { plan }: Props = $props();

	const hasScope = $derived(
		plan.scope &&
			((plan.scope.include?.length ?? 0) > 0 ||
				(plan.scope.exclude?.length ?? 0) > 0 ||
				(plan.scope.do_not_touch?.length ?? 0) > 0)
	);
</script>

<div class="plan-panel">
	<h3 class="panel-title">Plan Details</h3>

	<div class="plan-sections">
		{#if plan.goal}
			<div class="plan-section">
				<div class="section-header">
					<Icon name="target" size={14} />
					<span class="section-label">Goal</span>
				</div>
				<p class="section-content">{plan.goal}</p>
			</div>
		{/if}

		{#if plan.context}
			<div class="plan-section">
				<div class="section-header">
					<Icon name="info" size={14} />
					<span class="section-label">Context</span>
				</div>
				<p class="section-content">{plan.context}</p>
			</div>
		{/if}

		{#if hasScope}
			<div class="plan-section">
				<div class="section-header">
					<Icon name="folder" size={14} />
					<span class="section-label">Scope</span>
				</div>
				<div class="scope-content">
					{#if (plan.scope?.include?.length ?? 0) > 0}
						<div class="scope-group">
							<span class="scope-label include">Include</span>
							<ul class="scope-list">
								{#each plan.scope?.include ?? [] as path}
									<li class="scope-item">{path}</li>
								{/each}
							</ul>
						</div>
					{/if}

					{#if (plan.scope?.exclude?.length ?? 0) > 0}
						<div class="scope-group">
							<span class="scope-label exclude">Exclude</span>
							<ul class="scope-list">
								{#each plan.scope?.exclude ?? [] as path}
									<li class="scope-item">{path}</li>
								{/each}
							</ul>
						</div>
					{/if}

					{#if (plan.scope?.do_not_touch?.length ?? 0) > 0}
						<div class="scope-group">
							<span class="scope-label protected">Protected</span>
							<ul class="scope-list">
								{#each plan.scope?.do_not_touch ?? [] as path}
									<li class="scope-item">{path}</li>
								{/each}
							</ul>
						</div>
					{/if}
				</div>
			</div>
		{/if}

		{#if !plan.goal && !plan.context && !hasScope}
			<div class="empty-state">
				<Icon name="file-text" size={24} />
				<p>No plan details yet</p>
			</div>
		{/if}
	</div>
</div>

<style>
	.plan-panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.panel-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.plan-sections {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.plan-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-accent);
	}

	.section-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.section-content {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: var(--line-height-relaxed);
	}

	.scope-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.scope-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.scope-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		width: fit-content;
	}

	.scope-label.include {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.scope-label.exclude {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.scope-label.protected {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.scope-list {
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.scope-item {
		font-size: var(--font-size-sm);
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
		padding: var(--space-1) 0;
	}

	.scope-item::before {
		content: '• ';
		color: var(--color-text-muted);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6) 0;
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
	}
</style>
