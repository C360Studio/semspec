<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { PlanWithStatus, PlanStage } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
		hasRequirements?: boolean;
		hasScenarios?: boolean;
		onPromote: () => Promise<void>;
		onExecute: () => Promise<void>;
		onReplay?: () => Promise<void>;
	}

	let { plan, hasRequirements = false, hasScenarios = false, onPromote, onExecute, onReplay }: Props = $props();

	// Button visibility logic
	// Round 1: plan reviewed, waiting for human approval (stage=reviewed, approved=false)
	const showApprovePlan = $derived(!plan.approved && !!plan.goal);

	// Cascade: approved but requirements/scenarios not yet generated
	const isCascading = $derived(
		plan.approved &&
			!hasScenarios &&
			['approved', 'requirements_generated', 'scenarios_generated'].includes(plan.stage)
	);

	const cascadeLabel = $derived.by(() => {
		if (hasRequirements && !hasScenarios) return 'Generating scenarios...';
		switch (plan.stage) {
			case 'approved':
				return 'Generating requirements...';
			case 'requirements_generated':
				return 'Generating scenarios...';
			case 'scenarios_generated':
				return 'Reviewing scenarios...';
			default:
				return '';
		}
	});

	// Round 2: scenarios reviewed, waiting for human approval (stage=scenarios_reviewed)
	const showApproveScenarios = $derived(
		plan.approved &&
			hasScenarios &&
			plan.stage === 'scenarios_reviewed'
	);

	// Execute: ready only after second approval (ready_for_execution)
	const showExecute = $derived(
		plan.approved &&
			hasScenarios &&
			plan.stage === 'ready_for_execution'
	);

	// Show executing status when plan is actively running
	const isExecuting = $derived(
		plan.approved && ['implementing', 'executing', 'reviewing_rollup'].includes(plan.stage)
	);

	// Replay when plan has failed
	const showReplay = $derived(
		plan.approved && plan.stage === 'failed' && !!onReplay
	);

	// Loading states
	let promoteLoading = $state(false);
	let executeLoading = $state(false);
	let replayLoading = $state(false);

	async function handlePromote() {
		promoteLoading = true;
		try {
			await onPromote();
		} finally {
			promoteLoading = false;
		}
	}

	async function handleExecute() {
		executeLoading = true;
		try {
			await onExecute();
		} finally {
			executeLoading = false;
		}
	}

	async function handleReplay() {
		replayLoading = true;
		try {
			await onReplay?.();
		} finally {
			replayLoading = false;
		}
	}
</script>

{#if showApprovePlan || isCascading || showApproveScenarios || showExecute || isExecuting || showReplay || plan.stage === 'complete'}
	<div class="action-bar">
		{#if showApprovePlan}
			<button
				class="action-btn btn-primary"
				onclick={handlePromote}
				disabled={promoteLoading}
				aria-busy={promoteLoading}
			>
				<Icon name="arrow-up" size={16} />
				<span>Create Requirements</span>
			</button>
		{/if}

		{#if isCascading}
			<div class="cascade-status" role="status">
				<Icon name="loader" size={16} />
				<span>{cascadeLabel}</span>
			</div>
		{/if}

		{#if showApproveScenarios}
			<button
				class="action-btn btn-primary"
				onclick={handlePromote}
				disabled={promoteLoading}
				aria-busy={promoteLoading}
			>
				<Icon name="check-circle" size={16} />
				<span>Approve & Continue</span>
			</button>
		{/if}

		{#if showExecute}
			<button
				class="action-btn btn-success"
				onclick={handleExecute}
				disabled={executeLoading}
				aria-busy={executeLoading}
			>
				<Icon name="play" size={16} />
				<span>Start Execution</span>
			</button>
		{/if}

		{#if isExecuting}
			<div class="execution-status" role="status">
				<Icon name="loader" size={16} />
				<span>Executing...</span>
			</div>
		{/if}

		{#if plan.stage === 'complete'}
			<div class="complete-status" role="status">
				<Icon name="check-circle" size={16} />
				<span>Complete</span>
			</div>
		{/if}

		{#if showReplay}
			<button
				class="action-btn btn-warning"
				onclick={handleReplay}
				disabled={replayLoading}
				aria-busy={replayLoading}
			>
				<Icon name="refresh-cw" size={16} />
				<span>Replay</span>
			</button>
		{/if}
	</div>
{/if}

<style>
	.action-bar {
		display: flex;
		gap: var(--space-3);
		flex-wrap: wrap;
		align-items: center;
	}

	.action-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
		white-space: nowrap;
	}

	.action-btn:hover:not(:disabled) {
		opacity: 0.9;
		transform: translateY(-1px);
		box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
	}

	.action-btn:active:not(:disabled) {
		transform: translateY(0);
	}

	.action-btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.action-btn[aria-busy='true'] {
		position: relative;
		padding-right: calc(var(--space-4) + 20px);
	}

	.action-btn[aria-busy='true']::after {
		content: '';
		position: absolute;
		right: var(--space-3);
		top: 50%;
		transform: translateY(-50%);
		width: 14px;
		height: 14px;
		border: 2px solid currentColor;
		border-right-color: transparent;
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-success {
		background: var(--color-success);
		color: white;
	}

	.btn-warning {
		background: var(--color-warning);
		color: var(--color-bg-primary);
	}

	.cascade-status {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent-muted);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-accent);
	}

	.cascade-status :global(svg),
	.execution-status :global(svg) {
		animation: spin 1s linear infinite;
	}

	.execution-status {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-success-muted);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-success);
	}

	.complete-status {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-success-muted);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-success);
		font-weight: var(--font-weight-medium);
	}

	@media (max-width: 600px) {
		.action-bar {
			flex-direction: column;
		}

		.action-btn {
			width: 100%;
			justify-content: center;
		}
	}
</style>
