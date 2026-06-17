<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import {
		executionAttemptModel,
		executionBlockers,
		qaOutcomeState,
		storyTaskCounts,
		type ExecutionTask,
		type ExecutionTaskAttempt,
		type ExecutionTaskGroupStatus
	} from '$lib/components/plan/observabilityModels';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
		executionTasks?: ExecutionTask[];
	}

	let { plan, executionTasks = [] }: Props = $props();

	type Story = NonNullable<PlanWithStatus['stories']>[number];
	type StoryTask = NonNullable<Story['tasks']>[number];

	const summary = $derived(plan.phase_summary ?? null);
	const stories = $derived(plan.stories ?? []);
	const activeLoops = $derived(plan.active_loops ?? []);
	const execution = $derived(summary?.execution ?? plan.execution_summary ?? null);
	const qaRun = $derived(plan.qa_run ?? null);
	const qaVerdict = $derived(plan.qa_verdict_summary ?? null);
	const qaState = $derived(qaOutcomeState(plan));
	const attemptModel = $derived(executionAttemptModel(executionTasks));
	const skippedTests = $derived(qaRun?.skipped_tests ?? []);
	const skipGuardTriggered = $derived(
		skippedTests.length > 0 &&
			((qaVerdict?.summary ?? '').includes('[skip-guard]') || qaVerdict?.verdict === 'needs_changes')
	);
	const terminalState = $derived(
		summary?.phase === 'terminal' || ['complete', 'complete_with_deferrals', 'failed'].includes(plan.stage)
	);
	const blockers = $derived(executionBlockers(plan));

	function statusClass(status?: string): string {
		switch (status) {
			case 'complete':
			case 'completed':
			case 'approved':
			case 'passed':
				return 'success';
			case 'failed':
			case 'error':
			case 'rejected':
				return 'error';
			case 'executing':
			case 'in_progress':
			case 'running':
			case 'active':
				return 'active';
			case 'blocked':
			case 'waiting':
			case 'recovered':
				return 'warning';
			default:
				return 'neutral';
		}
	}

	function compactStatus(status?: string): string {
		return status ? status.replaceAll('_', ' ') : 'pending';
	}

	function loopLabel(loop: PlanWithStatus['active_loops'][number]): string {
		return [loop.role, loop.state].filter(Boolean).join(' · ');
	}

	function taskTitle(task: StoryTask): string {
		return task.description || task.id;
	}

	function groupStatusLabel(status: ExecutionTaskGroupStatus): string {
		switch (status) {
			case 'active':
				return 'active';
			case 'approved':
				return 'approved';
			case 'recovered':
				return 'recovered';
			case 'blocked':
				return 'blocked';
			default:
				return 'pending';
		}
	}

	function attemptState(attempt: ExecutionTaskAttempt): string {
		if (attempt.stage === 'approved') return 'success';
		if (attempt.stage === 'escalated') return 'warning';
		if (attempt.stage === 'error' || attempt.stage === 'rejected') return 'error';
		return 'active';
	}

	function attemptIcon(attempt: ExecutionTaskAttempt): string {
		switch (attemptState(attempt)) {
			case 'success':
				return 'check-circle';
			case 'warning':
				return 'alert-triangle';
			case 'error':
				return 'x-circle';
			default:
				return 'loader';
		}
	}

	function cycleLabel(attempt: ExecutionTaskAttempt): string {
		if (typeof attempt.tddCycle !== 'number') return '';
		const current = attempt.tddCycle + 1;
		if (typeof attempt.maxTddCycles === 'number') return `cycle ${current}/${attempt.maxTddCycles}`;
		return `cycle ${current}`;
	}

	function shortRequirement(id: string | undefined): string {
		if (!id) return 'Plan';
		const match = id.match(/(\d+)$/);
		return match ? `Req ${match[1]}` : id;
	}

	function shortSha(sha: string | undefined): string {
		return sha ? sha.slice(0, 8) : '';
	}

	function timeLabel(value: string | undefined): string {
		if (!value) return '';
		const date = new Date(value);
		if (!Number.isFinite(date.getTime())) return '';
		return `${date.toISOString().slice(11, 19)}Z`;
	}

	function attemptDetail(attempt: ExecutionTaskAttempt): string {
		return attempt.errorReason || attempt.escalationReason || attempt.feedback || '';
	}
</script>

<section class="execution-detail" aria-label="Execution detail">
	<div class="section-header">
		<div class="section-title">
			<Icon name="play" size={14} />
			<span>Execution Detail</span>
		</div>
		{#if summary}
			<span class="phase-pill" data-phase={summary.phase} data-state={summary.state}>
				{summary.title}
			</span>
		{/if}
	</div>

	<div class="overview-grid">
		<div class="overview-item">
			<span class="overview-label">Stories</span>
			<span class="overview-value">{stories.length}</span>
		</div>
		<div class="overview-item">
			<span class="overview-label">Tasks</span>
			<span class="overview-value">
				{stories.reduce((sum, story) => sum + (story.tasks?.length ?? 0), 0)}
			</span>
		</div>
		<div class="overview-item">
			<span class="overview-label">Loops</span>
			<span class="overview-value">{activeLoops.length}</span>
		</div>
		{#if attemptModel.totalAttempts > 0}
			<div class="overview-item">
				<span class="overview-label">Attempts</span>
				<span class="overview-value">{attemptModel.totalAttempts}</span>
			</div>
			<div class="overview-item" data-state={attemptModel.orphanedGroups > 0 ? 'warning' : 'neutral'}>
				<span class="overview-label">Recovered</span>
				<span class="overview-value">{attemptModel.orphanedGroups}</span>
			</div>
		{/if}
		{#if execution}
			<div class="overview-item">
				<span class="overview-label">Reqs Done</span>
				<span class="overview-value">{execution.completed}/{execution.total}</span>
			</div>
			<div class="overview-item" data-state={execution.failed > 0 ? 'error' : 'neutral'}>
				<span class="overview-label">Failed</span>
				<span class="overview-value">{execution.failed}</span>
			</div>
		{/if}
	</div>

	{#if blockers.length > 0 || attemptModel.warnings.length > 0 || skipGuardTriggered}
		<div class="blockers">
			{#each blockers as blocker}
				<div class="blocker" data-kind={blocker.kind}>
					<Icon name={blocker.kind === 'wait' ? 'pause' : 'alert-triangle'} size={14} />
					<div>
						<div class="blocker-label">{blocker.label}</div>
						{#if blocker.detail}<div class="blocker-detail">{blocker.detail}</div>{/if}
					</div>
				</div>
			{/each}
			{#if skipGuardTriggered}
				<div class="blocker" data-kind="qa">
					<Icon name="alert-triangle" size={14} />
					<div>
						<div class="blocker-label">Skipped tests need classification</div>
						<div class="blocker-detail">
							{skippedTests.length} skipped test{skippedTests.length === 1 ? '' : 's'} kept QA from treating this as all-green.
						</div>
					</div>
				</div>
			{/if}
			{#each attemptModel.warnings as warning (warning.relatedId)}
				<div class="blocker" data-kind="attempt">
					<Icon name="alert-triangle" size={14} />
					<div>
						<div class="blocker-label">{warning.title}</div>
						<div class="blocker-detail">{warning.detail}</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}

	{#if activeLoops.length > 0}
		<div class="subsection">
			<div class="subsection-title">
				<Icon name="activity" size={13} />
				<span>Active Loops</span>
			</div>
			<div class="loop-list">
				{#each activeLoops as loop (loop.loop_id)}
					<div class="loop-row" data-state={statusClass(loop.state)}>
						<span class="loop-main">{loopLabel(loop)}</span>
						<span class="loop-id">{loop.loop_id.slice(0, 12)}</span>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	{#if attemptModel.taskGroups.length > 0}
		<div class="subsection">
			<div class="subsection-title">
				<Icon name="git-branch" size={13} />
				<span>Execution Attempts</span>
			</div>
			<div class="attempt-group-list">
				{#each attemptModel.taskGroups as group (group.key)}
					<div class="attempt-group" data-status={group.status}>
						<div class="attempt-group-main">
							<span class="req-chip">{shortRequirement(group.requirementId)}</span>
							<span class="attempt-title">{group.title}</span>
							<span class="status-pill" data-state={statusClass(group.status)}>
								{groupStatusLabel(group.status)}
							</span>
						</div>
						<div class="attempt-list">
							{#each group.attempts as attempt, index (attempt.taskId)}
								<div class="attempt-row" data-state={attemptState(attempt)}>
									<Icon name={attemptIcon(attempt)} size={13} />
									<span>Attempt {index + 1}</span>
									<span class="attempt-stage">{compactStatus(attempt.stage)}</span>
									{#if cycleLabel(attempt)}
										<span>{cycleLabel(attempt)}</span>
									{/if}
									{#if attempt.mergeCommit}
										<span class="attempt-sha">
											<Icon name="git-commit" size={12} />
											{shortSha(attempt.mergeCommit)}
										</span>
									{/if}
									{#if attempt.updatedAt}
										<span class="attempt-time">{timeLabel(attempt.updatedAt)}</span>
									{/if}
								</div>
								{#if attemptDetail(attempt)}
									<div class="attempt-detail">{attemptDetail(attempt)}</div>
								{/if}
							{/each}
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	{#if stories.length > 0}
		<div class="subsection">
			<div class="subsection-title">
				<Icon name="list-checks" size={13} />
				<span>Stories And Tasks</span>
			</div>
			<div class="story-list">
				{#each stories as story (story.id)}
					{@const counts = storyTaskCounts(story)}
					<details class="story-card" open={statusClass(story.status) !== 'success'}>
						<summary>
							<div class="story-summary">
								<span class="story-title">{story.title}</span>
								<span class="status-pill" data-state={statusClass(story.status)}>
									{compactStatus(story.status)}
								</span>
							</div>
							<div class="story-meta">
								<span>{counts.done}/{counts.total} tasks</span>
								{#if counts.active > 0}<span>{counts.active} active</span>{/if}
								{#if counts.failed > 0}<span>{counts.failed} failed</span>{/if}
								{#if story.files_owned?.length}<span>{story.files_owned.length} files</span>{/if}
							</div>
						</summary>
						{#if story.tasks?.length}
							<ul class="task-list">
								{#each story.tasks as task (task.id)}
									<li class="task-row">
										<span class="status-dot" data-state={statusClass(task.status)}></span>
										<span class="task-title">{taskTitle(task)}</span>
										<span class="task-status">{compactStatus(task.status)}</span>
									</li>
								{/each}
							</ul>
						{:else}
							<div class="empty-note">No task records yet</div>
						{/if}
					</details>
				{/each}
			</div>
		</div>
	{/if}

	{#if qaRun || qaVerdict || terminalState}
		<div class="subsection">
			<div class="subsection-title">
				<Icon name="test-tube" size={13} />
				<span>QA And Outcome</span>
			</div>
			<div class="qa-box" data-state={qaState}>
				{#if terminalState}
					<div class="qa-row">
						<span>Terminal</span>
						<strong>{compactStatus(plan.stage)}</strong>
					</div>
				{/if}
				{#if qaRun}
					<div class="qa-row">
						<span>QA run</span>
						<strong>{qaRun.passed ? 'passed' : 'failed'}</strong>
					</div>
					{#if qaRun.failures?.length}
						<ul class="qa-failures">
							{#each qaRun.failures as failure}
								<li>
									<span>{failure.category ?? failure.job_name}</span>
									{#if failure.message}<small>{failure.message}</small>{/if}
								</li>
							{/each}
						</ul>
					{/if}
					{#if skippedTests.length}
						<div class="skip-list">
							<div class="skip-title">
								<Icon name="skip-forward" size={13} />
								<span>{skippedTests.length} skipped test{skippedTests.length === 1 ? '' : 's'}</span>
							</div>
							{#each skippedTests.slice(0, 4) as skipped, index (skipped.name + index)}
								<span class="skip-item">{skipped.name}</span>
							{/each}
						</div>
					{/if}
				{/if}
				{#if qaVerdict}
					<div class="qa-row">
						<span>Verdict</span>
						<strong>{qaVerdict.verdict}</strong>
					</div>
					{#if qaVerdict.summary}<p class="qa-summary">{qaVerdict.summary}</p>{/if}
				{/if}
			</div>
		</div>
	{/if}
</section>

<style>
	.execution-detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-bg-secondary);
	}

	.section-header,
	.subsection-title,
	.story-summary,
	.story-meta,
	.loop-row,
	.task-row,
	.qa-row,
	.blocker {
		display: flex;
		align-items: center;
	}

	.section-header {
		justify-content: space-between;
		gap: var(--space-3);
	}

	.section-title,
	.subsection-title {
		gap: var(--space-2);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.section-title {
		font-size: var(--font-size-sm);
	}

	.subsection-title {
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	.phase-pill,
	.status-pill {
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		white-space: nowrap;
	}

	.phase-pill[data-phase='execution'],
	.status-pill[data-state='active'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.status-pill[data-state='success'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.status-pill[data-state='error'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.overview-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(90px, 1fr));
		gap: var(--space-2);
	}

	.overview-item {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
	}

	.overview-item[data-state='error'] .overview-value {
		color: var(--color-error);
	}

	.overview-item[data-state='warning'] .overview-value {
		color: var(--color-warning);
	}

	.overview-label,
	.story-meta,
	.loop-id,
	.task-status,
	.attempt-row,
	.attempt-detail,
	.empty-note,
	.qa-row span,
	.qa-failures small,
	.skip-item,
	.blocker-detail {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.overview-value {
		font-family: var(--font-family-mono);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.blockers,
	.loop-list,
	.story-list,
	.attempt-group-list,
	.attempt-list,
	.skip-list,
	.task-list,
	.qa-failures {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.blocker {
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.blocker[data-kind='error'],
	.blocker[data-kind='qa'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.12));
		color: var(--color-error);
	}

	.blocker[data-kind='attempt'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.blocker-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.subsection {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.loop-row {
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
	}

	.loop-main,
	.task-title,
	.story-title {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.loop-main,
	.task-title {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.story-card {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		overflow: hidden;
	}

	.story-card summary {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		cursor: pointer;
	}

	.story-summary,
	.story-meta {
		justify-content: space-between;
		gap: var(--space-2);
	}

	.story-meta {
		justify-content: flex-start;
		flex-wrap: wrap;
	}

	.task-list {
		list-style: none;
		margin: 0;
		padding: 0 var(--space-3) var(--space-2);
	}

	.attempt-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
	}

	.attempt-group-main {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: var(--space-2);
	}

	.req-chip {
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-primary);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		white-space: nowrap;
	}

	.attempt-title {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.attempt-list {
		gap: var(--space-1);
		padding-left: var(--space-3);
	}

	.attempt-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 22px;
	}

	.attempt-row[data-state='success'] {
		color: var(--color-success);
	}

	.attempt-row[data-state='warning'] {
		color: var(--color-warning);
	}

	.attempt-row[data-state='error'] {
		color: var(--color-error);
	}

	.attempt-row[data-state='active'] {
		color: var(--color-accent);
	}

	.attempt-stage,
	.attempt-sha,
	.attempt-time {
		color: var(--color-text-secondary);
	}

	.attempt-sha {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		font-family: var(--font-family-mono);
	}

	.attempt-detail {
		padding-left: calc(13px + var(--space-2));
		line-height: var(--line-height-normal);
	}

	.task-row {
		gap: var(--space-2);
		padding: var(--space-1) 0;
	}

	.status-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--color-text-muted);
		flex-shrink: 0;
	}

	.status-dot[data-state='success'] {
		background: var(--color-success);
	}

	.status-dot[data-state='error'] {
		background: var(--color-error);
	}

	.status-dot[data-state='active'] {
		background: var(--color-accent);
	}

	.status-dot[data-state='warning'] {
		background: var(--color-warning);
	}

	.task-status {
		margin-left: auto;
		white-space: nowrap;
	}

	.empty-note {
		padding: 0 var(--space-3) var(--space-2);
	}

	.qa-box {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		border: 1px solid transparent;
		background: var(--color-bg-tertiary);
	}

	.qa-box[data-state='success'] {
		border-color: color-mix(in srgb, var(--color-success) 35%, transparent);
		background: color-mix(in srgb, var(--color-success-muted) 45%, var(--color-bg-tertiary));
	}

	.qa-box[data-state='warning'] {
		border-color: color-mix(in srgb, var(--color-warning) 35%, transparent);
		background: color-mix(in srgb, var(--color-warning-muted) 45%, var(--color-bg-tertiary));
	}

	.qa-box[data-state='error'] {
		border-color: color-mix(in srgb, var(--color-error) 35%, transparent);
		background: color-mix(in srgb, var(--color-error-muted) 45%, var(--color-bg-tertiary));
	}

	.qa-row {
		justify-content: space-between;
		gap: var(--space-3);
	}

	.qa-row strong {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.qa-failures {
		margin: 0;
		padding-left: var(--space-4);
	}

	.qa-failures li {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.qa-failures small {
		display: block;
		margin-top: 2px;
	}

	.qa-summary {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-normal);
		color: var(--color-text-secondary);
	}

	.skip-list {
		gap: var(--space-1);
	}

	.skip-title {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-warning);
	}

	.skip-item {
		line-height: var(--line-height-normal);
	}

	@media (max-width: 720px) {
		.attempt-group-main {
			grid-template-columns: auto minmax(0, 1fr);
		}

		.attempt-group-main .status-pill {
			grid-column: 1 / -1;
			justify-self: flex-start;
		}

		.attempt-row {
			flex-wrap: wrap;
		}
	}
</style>
