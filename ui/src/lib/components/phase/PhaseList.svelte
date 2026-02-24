<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import PhaseCard from './PhaseCard.svelte';
	import PhaseEditModal from './PhaseEditModal.svelte';
	import TaskEditModal from '$lib/components/task/TaskEditModal.svelte';
	import type { Phase } from '$lib/types/phase';
	import type { Task } from '$lib/types/task';
	import type { ActiveLoop } from '$lib/types/plan';
	import { api } from '$lib/api/client';

	interface Props {
		phases: Phase[];
		tasks: Task[];
		planSlug: string;
		planApproved?: boolean;
		activeLoops?: ActiveLoop[];
		canAddPhase?: boolean;
		canGeneratePhases?: boolean;
		onPhasesChange?: () => Promise<void>;
		onTasksChange?: () => Promise<void>;
		onTaskApprove?: (taskId: string) => void | Promise<void>;
		onTaskReject?: (taskId: string, reason: string) => void | Promise<void>;
	}

	let {
		phases,
		tasks,
		planSlug,
		planApproved = false,
		activeLoops = [],
		canAddPhase = false,
		canGeneratePhases = false,
		onPhasesChange,
		onTasksChange,
		onTaskApprove,
		onTaskReject
	}: Props = $props();

	// Track which phases are expanded
	let expandedPhases = $state<Set<string>>(new Set());

	// Modal states
	let showPhaseModal = $state(false);
	let editingPhase = $state<Phase | undefined>(undefined);
	let showTaskModal = $state(false);
	let editingTask = $state<Task | undefined>(undefined);
	let taskPhaseId = $state<string | undefined>(undefined);

	// Loading states
	let generating = $state(false);
	let approvingAll = $state(false);

	// Stats
	const phaseStats = $derived.by(() => {
		return {
			total: phases.length,
			pending: phases.filter((p) => p.status === 'pending').length,
			ready: phases.filter((p) => p.status === 'ready').length,
			active: phases.filter((p) => p.status === 'active').length,
			complete: phases.filter((p) => p.status === 'complete').length,
			needsApproval: phases.filter((p) => p.requires_approval && !p.approved).length
		};
	});

	const hasUnapprovedPhases = $derived(phaseStats.needsApproval > 0);

	function togglePhase(phaseId: string) {
		const newExpanded = new Set(expandedPhases);
		if (newExpanded.has(phaseId)) {
			newExpanded.delete(phaseId);
		} else {
			newExpanded.add(phaseId);
		}
		expandedPhases = newExpanded;
	}

	function handleAddPhase() {
		editingPhase = undefined;
		showPhaseModal = true;
	}

	function handleEditPhase(phase: Phase) {
		editingPhase = phase;
		showPhaseModal = true;
	}

	async function handleDeletePhase(phase: Phase) {
		try {
			await api.phases.delete(planSlug, phase.id);
			await onPhasesChange?.();
		} catch (err) {
			console.error('Failed to delete phase:', err);
		}
	}

	async function handleApprovePhase(phase: Phase) {
		try {
			await api.phases.approve(planSlug, phase.id);
			await onPhasesChange?.();
		} catch (err) {
			console.error('Failed to approve phase:', err);
		}
	}

	async function handlePhaseModalSave() {
		await onPhasesChange?.();
	}

	function closePhaseModal() {
		showPhaseModal = false;
		editingPhase = undefined;
	}

	// Task handlers
	function handleAddTask(phaseId: string) {
		editingTask = undefined;
		taskPhaseId = phaseId;
		showTaskModal = true;
	}

	function handleEditTask(task: Task) {
		editingTask = task;
		taskPhaseId = task.phase_id;
		showTaskModal = true;
	}

	async function handleTaskModalSave() {
		await onTasksChange?.();
	}

	function closeTaskModal() {
		showTaskModal = false;
		editingTask = undefined;
		taskPhaseId = undefined;
	}

	// Generate phases
	async function handleGeneratePhases() {
		generating = true;
		try {
			await api.phases.generate(planSlug);
			await onPhasesChange?.();
		} catch (err) {
			console.error('Failed to generate phases:', err);
		} finally {
			generating = false;
		}
	}

	// Approve all phases
	async function handleApproveAll() {
		approvingAll = true;
		try {
			await api.phases.approveAll(planSlug);
			await onPhasesChange?.();
		} catch (err) {
			console.error('Failed to approve all phases:', err);
		} finally {
			approvingAll = false;
		}
	}
</script>

<div class="phase-list">
	<div class="phase-list-header">
		<h3 class="section-title">
			<Icon name="layers" size={16} />
			Phases
			{#if phases.length > 0}
				<span class="phase-count">({phases.length})</span>
			{/if}
		</h3>
		<div class="header-actions">
			{#if hasUnapprovedPhases}
				<button
					class="btn btn-success btn-sm"
					onclick={handleApproveAll}
					disabled={approvingAll}
					aria-busy={approvingAll}
				>
					<Icon name="check-circle" size={14} />
					Approve All ({phaseStats.needsApproval})
				</button>
			{/if}
			{#if canAddPhase}
				<button class="btn btn-primary btn-sm" onclick={handleAddPhase}>
					<Icon name="plus" size={14} />
					Add Phase
				</button>
			{/if}
		</div>
	</div>

	{#if phases.length === 0}
		<div class="empty-state">
			{#if canGeneratePhases}
				<Icon name="layers" size={32} />
				<p>No phases yet. Generate phases from the plan to organize tasks.</p>
				<button
					class="btn btn-primary"
					onclick={handleGeneratePhases}
					disabled={generating}
					aria-busy={generating}
				>
					{#if generating}
						<Icon name="loader" size={14} />
						Generating...
					{:else}
						<Icon name="zap" size={14} />
						Generate Phases
					{/if}
				</button>
			{:else if !planApproved}
				<Icon name="lock" size={32} />
				<p>Approve the plan to enable phase generation.</p>
			{:else}
				<Icon name="layers" size={32} />
				<p>No phases defined yet.</p>
			{/if}
		</div>
	{:else}
		<div class="phase-cards">
			{#each phases as phase (phase.id)}
				<PhaseCard
					{phase}
					{tasks}
					allPhases={phases}
					{activeLoops}
					expanded={expandedPhases.has(phase.id)}
					onToggle={() => togglePhase(phase.id)}
					onEdit={handleEditPhase}
					onDelete={handleDeletePhase}
					onApprove={handleApprovePhase}
					onTaskApprove={onTaskApprove}
					onTaskReject={onTaskReject}
					onTaskEdit={handleEditTask}
					onAddTask={handleAddTask}
				/>
			{/each}
		</div>
	{/if}
</div>

<PhaseEditModal
	open={showPhaseModal}
	phase={editingPhase}
	{planSlug}
	allPhases={phases}
	onClose={closePhaseModal}
	onSave={handlePhaseModalSave}
/>

<TaskEditModal
	open={showTaskModal}
	task={editingTask}
	{planSlug}
	allTasks={tasks}
	phaseId={taskPhaseId}
	onClose={closeTaskModal}
	onSave={handleTaskModalSave}
/>

<style>
	.phase-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.phase-list-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.section-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0;
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.phase-count {
		font-weight: var(--font-weight-normal);
		color: var(--color-text-muted);
	}

	.header-actions {
		display: flex;
		gap: var(--space-2);
	}

	.phase-cards {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-8) var(--space-4);
		background: var(--color-bg-secondary);
		border: 2px dashed var(--color-border);
		border-radius: var(--radius-lg);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state p {
		margin: 0;
		max-width: 300px;
	}

	/* Buttons */
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.btn-sm {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.btn-success {
		background: var(--color-success);
		color: white;
	}

	.btn-success:hover:not(:disabled) {
		background: var(--color-success-hover, #059669);
	}
</style>
