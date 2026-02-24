<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';

	interface Props {
		plan: PlanWithStatus;
		tasks: Task[];
		onPromote: () => Promise<void>;
		onGenerateTasks: () => Promise<void>;
		onApproveAll: () => Promise<void>;
		onExecute: () => Promise<void>;
	}

	let { plan, tasks, onPromote, onGenerateTasks, onApproveAll, onExecute }: Props = $props();

	// Button visibility logic
	const showApprovePlan = $derived(!plan.approved && !!plan.goal);
	const showGenerateTasks = $derived(
		plan.approved &&
			['approved', 'reviewed'].includes(plan.stage) &&
			tasks.length === 0
	);

	const pendingApprovalCount = $derived(
		tasks.filter((t) => t.status === 'pending_approval').length
	);
	const showApproveAll = $derived(pendingApprovalCount > 0);

	const allTasksApproved = $derived(
		tasks.length > 0 && tasks.every((t) => t.status === 'approved' || t.status === 'completed')
	);
	const approvedCount = $derived(tasks.filter((t) => t.status === 'approved').length);
	const showExecute = $derived(
		allTasksApproved && ['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage)
	);

	// Loading states
	let promoteLoading = $state(false);
	let generateLoading = $state(false);
	let approveAllLoading = $state(false);
	let executeLoading = $state(false);

	async function handlePromote() {
		promoteLoading = true;
		try {
			await onPromote();
		} finally {
			promoteLoading = false;
		}
	}

	async function handleGenerateTasks() {
		generateLoading = true;
		try {
			await onGenerateTasks();
		} finally {
			generateLoading = false;
		}
	}

	async function handleApproveAll() {
		approveAllLoading = true;
		try {
			await onApproveAll();
		} finally {
			approveAllLoading = false;
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
</script>

{#if showApprovePlan || showGenerateTasks || showApproveAll || showExecute}
	<div class="action-bar">
		{#if showApprovePlan}
			<button
				class="action-btn btn-primary"
				onclick={handlePromote}
				disabled={promoteLoading}
				aria-busy={promoteLoading}
			>
				<Icon name="arrow-up" size={16} />
				<span>Approve Plan</span>
			</button>
		{/if}

		{#if showGenerateTasks}
			<button
				class="action-btn btn-primary"
				onclick={handleGenerateTasks}
				disabled={generateLoading}
				aria-busy={generateLoading}
			>
				<Icon name="list" size={16} />
				<span>Generate Tasks</span>
			</button>
		{/if}

		{#if showApproveAll}
			<button
				class="action-btn btn-primary"
				onclick={handleApproveAll}
				disabled={approveAllLoading}
				aria-busy={approveAllLoading}
			>
				<Icon name="clock" size={16} />
				<span>Approve All ({pendingApprovalCount})</span>
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
				<span>Start Execution ({approvedCount} task{approvedCount === 1 ? '' : 's'})</span>
			</button>
		{/if}
	</div>
{/if}

<style>
	.action-bar {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-4) 0;
		margin-bottom: var(--space-4);
		flex-wrap: wrap;
	}

	.action-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-5);
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
		padding-right: calc(var(--space-4) + 20px); /* Extra space for spinner */
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

	/* Responsive: stack buttons on mobile */
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
