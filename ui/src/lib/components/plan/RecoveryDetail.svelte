<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
	}

	type RecoveryDecision = NonNullable<PlanWithStatus['plan_decisions']>[number];

	let { plan }: Props = $props();

	const recovery = $derived(plan.phase_summary?.recovery ?? null);
	const wait = $derived(plan.phase_summary?.wait ?? null);
	const decisions = $derived(plan.plan_decisions ?? []);
	const currentDecision = $derived.by(() => {
		if (recovery?.decision_id) {
			return decisions.find((decision) => decision.id === recovery.decision_id) ?? null;
		}
		return decisions[0] ?? null;
	});
	const affectedNodes = $derived.by(() => {
		const decision = currentDecision;
		if (!decision) return [];
		return [
			...(decision.affected_requirement_ids ?? []).map((id) => ({ kind: 'Requirement', id })),
			...(decision.affected_story_ids ?? []).map((id) => ({ kind: 'Story', id })),
			...(decision.contract_impact?.affected_ids ?? []).map((id) => ({ kind: 'Contract', id }))
		];
	});
	const autoAccept = $derived(currentDecision ? inferAutoAccept(currentDecision) : null);

	function inferAutoAccept(decision: RecoveryDecision): { label: string; detail: string; state: 'success' | 'warning' | 'neutral' } {
		const impactKind = decision.contract_impact?.kind;
		const scoped = (decision.affected_requirement_ids?.length ?? 0) > 0 || (decision.affected_story_ids?.length ?? 0) > 0;
		if (decision.status === 'accepted') {
			return {
				label: 'Accepted',
				detail: decision.proposed_by === 'recovery-agent'
					? 'Recovery decision has been accepted; auto-accept may have applied if policy allowed it.'
					: 'Decision has been accepted.',
				state: 'success'
			};
		}
		if (decision.status === 'proposed' || decision.status === 'under_review') {
			if (!impactKind || impactKind === 'change') {
				return {
					label: 'Review required',
					detail: 'Auto-accept is not inferred for missing or contract-changing impact.',
					state: 'warning'
				};
			}
			if (!scoped) {
				return {
					label: 'Review required',
					detail: 'Auto-accept requires scoped affected nodes.',
					state: 'warning'
				};
			}
			return {
				label: 'Policy eligible',
				detail: 'Preserve/refine impact with scoped nodes; final auto-accept depends on recovery policy budget.',
				state: 'neutral'
			};
		}
		return {
			label: 'Not active',
			detail: 'Decision is no longer awaiting recovery policy action.',
			state: 'neutral'
		};
	}

	function formatStatus(status?: string): string {
		return status ? status.replaceAll('_', ' ') : 'unknown';
	}
</script>

<section class="recovery-detail" aria-label="Recovery and PlanDecision detail">
	<div class="section-header">
		<div class="section-title">
			<Icon name="git-pull-request" size={14} />
			<span>Recovery Detail</span>
		</div>
		{#if currentDecision}
			<span class="status-pill" data-state={currentDecision.status}>
				{formatStatus(currentDecision.status)}
			</span>
		{/if}
	</div>

	{#if currentDecision}
		<div class="decision-card">
			<div class="decision-heading">
				<div>
					<h3>{currentDecision.title}</h3>
					<div class="decision-meta">
						<span>{currentDecision.kind ?? 'plan_decision'}</span>
						<span>by {currentDecision.proposed_by}</span>
					</div>
				</div>
				{#if autoAccept}
					<div class="auto-accept" data-state={autoAccept.state}>
						<span>{autoAccept.label}</span>
					</div>
				{/if}
			</div>

			<div class="diagnosis">
				<span class="field-label">Diagnosis</span>
				<p>{recovery?.summary ?? currentDecision.rationale}</p>
			</div>

			{#if currentDecision.contract_impact || recovery?.contract_impact_kind}
				<div class="contract-impact">
					<span class="field-label">Contract Impact</span>
					<div class="impact-row">
						<span class="impact-kind">
							{currentDecision.contract_impact?.kind ?? recovery?.contract_impact_kind}
						</span>
						<span>
							{currentDecision.contract_impact?.summary ?? recovery?.contract_impact_summary ?? 'No summary supplied'}
						</span>
					</div>
				</div>
			{/if}

			{#if affectedNodes.length > 0}
				<div class="affected">
					<span class="field-label">Affected Nodes</span>
					<div class="node-chips">
						{#each affectedNodes as node}
							<span class="node-chip">{node.kind}: {node.id}</span>
						{/each}
					</div>
				</div>
			{/if}

			{#if wait?.policy_reason}
				<div class="policy-note">
					<Icon name="pause" size={14} />
					<span>{wait.policy_reason}</span>
				</div>
			{:else if autoAccept}
				<div class="policy-note">
					<Icon name={autoAccept.state === 'success' ? 'check-circle' : 'info'} size={14} />
					<span>{autoAccept.detail}</span>
				</div>
			{/if}
		</div>
	{:else}
		<div class="empty-note">
			<Icon name="git-pull-request" size={18} />
			<span>No PlanDecision evidence is attached to this plan yet.</span>
		</div>
	{/if}
</section>

<style>
	.recovery-detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-bg-secondary);
	}

	.section-header,
	.section-title,
	.decision-heading,
	.impact-row,
	.policy-note,
	.empty-note {
		display: flex;
		align-items: center;
	}

	.section-header,
	.decision-heading {
		justify-content: space-between;
		gap: var(--space-3);
	}

	.section-title {
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.status-pill,
	.auto-accept,
	.impact-kind,
	.node-chip {
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	.status-pill[data-state='accepted'],
	.auto-accept[data-state='success'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.status-pill[data-state='rejected'],
	.auto-accept[data-state='warning'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.12));
		color: var(--color-warning);
	}

	.decision-card {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.decision-heading h3 {
		margin: 0;
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.decision-meta,
	.field-label,
	.empty-note {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.decision-meta {
		display: flex;
		gap: var(--space-2);
		margin-top: 2px;
	}

	.field-label {
		display: block;
		margin-bottom: var(--space-1);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.diagnosis p {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-normal);
		color: var(--color-text-secondary);
	}

	.impact-row {
		align-items: flex-start;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.node-chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
	}

	.policy-note {
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.empty-note {
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-4);
	}
</style>
