<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import {
		formatDuration,
		formatTokens,
		summarizeRunVisibility,
		type ExecutionTask,
		type Lesson,
		type RunStatusTone,
		type TaskAttemptSummary,
		type TaskGroupStatus
	} from '$lib/types/runVisibility';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { TrajectoryListItem } from '$lib/types/trajectory';

	interface Props {
		plan: PlanWithStatus;
		executionTasks?: ExecutionTask[];
		trajectoryItems?: TrajectoryListItem[];
		lessons?: Lesson[];
	}

	let {
		plan,
		executionTasks = [],
		trajectoryItems = [],
		lessons = []
	}: Props = $props();

	const visibility = $derived(
		summarizeRunVisibility(plan, executionTasks, trajectoryItems, lessons)
	);
	const displayedWarnings = $derived(visibility.warnings.slice(0, 4));
	const displayedLessons = $derived(visibility.lessons.slice(0, 6));

	function statusIcon(tone: RunStatusTone): string {
		switch (tone) {
			case 'active':
				return 'loader';
			case 'success':
				return 'check-circle';
			case 'warning':
				return 'alert-triangle';
			case 'danger':
				return 'x-circle';
			default:
				return 'activity';
		}
	}

	function groupStatusLabel(status: TaskGroupStatus): string {
		switch (status) {
			case 'active':
				return 'Active';
			case 'approved':
				return 'Approved';
			case 'recovered':
				return 'Recovered';
			case 'blocked':
				return 'Blocked';
			default:
				return 'Pending';
		}
	}

	function attemptIcon(attempt: TaskAttemptSummary): string {
		switch (attempt.stage) {
			case 'approved':
				return 'check-circle';
			case 'error':
			case 'rejected':
				return 'x-circle';
			case 'escalated':
				return 'alert-triangle';
			default:
				return 'loader';
		}
	}

	function attemptTone(attempt: TaskAttemptSummary): string {
		switch (attempt.stage) {
			case 'approved':
				return 'success';
			case 'error':
			case 'rejected':
				return 'danger';
			case 'escalated':
				return 'warning';
			default:
				return 'active';
		}
	}

	function cycleLabel(attempt: TaskAttemptSummary): string {
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

	function timeLabel(value: string | null | undefined): string {
		if (!value) return '';
		const date = new Date(value);
		if (!Number.isFinite(date.getTime())) return '';
		return `${date.toISOString().slice(11, 19)}Z`;
	}

	function clip(text: string | undefined, max = 190): string {
		if (!text) return '';
		const compact = text.replace(/\s+/g, ' ').trim();
		return compact.length > max ? `${compact.slice(0, max - 1)}...` : compact;
	}
</script>

{#if visibility.shouldRender}
	<section class="run-visibility" data-testid="run-visibility-panel">
		<div class="status-band" data-tone={visibility.status.tone} role="status" aria-live="polite">
			<div class="status-icon" aria-hidden="true">
				<Icon name={statusIcon(visibility.status.tone)} size={22} />
			</div>
			<div class="status-copy">
				<span class="eyebrow">Run state</span>
				<h2>{visibility.status.title}</h2>
				{#if visibility.status.detail}
					<p>{clip(visibility.status.detail, 260)}</p>
				{/if}
			</div>
			<div class="status-meta">
				<span class="stage-chip">{visibility.status.stageLabel}</span>
				{#if visibility.status.timestamp}
					<span class="time-chip">
						<Icon name="clock" size={13} />
						{timeLabel(visibility.status.timestamp)}
					</span>
				{/if}
			</div>
		</div>

		<div class="metric-row" aria-label="Run metrics">
			<div class="metric">
				<span>Tasks</span>
				<strong>{visibility.taskStats.approvedGroups}/{visibility.taskStats.totalGroups}</strong>
			</div>
			<div class="metric">
				<span>Attempts</span>
				<strong>{visibility.taskStats.totalAttempts}</strong>
			</div>
			<div class="metric" class:metric-alert={visibility.warnings.length > 0}>
				<span>Signals</span>
				<strong>{visibility.warnings.length}</strong>
			</div>
			<div class="metric">
				<span>Tokens</span>
				<strong>{formatTokens(visibility.usage.totalTokens)}</strong>
			</div>
			<div class="metric">
				<span>Cost</span>
				<strong>{visibility.usage.costLabel}</strong>
			</div>
		</div>

		{#if displayedWarnings.length > 0}
			<div class="warning-lane" aria-label="Run warnings">
				{#each displayedWarnings as warning (warning.kind + warning.relatedId)}
					<div class="warning-row" data-tone={warning.tone}>
						<Icon name={warning.tone === 'danger' ? 'x-circle' : 'alert-triangle'} size={15} />
						<div>
							<strong>{warning.title}</strong>
							<span>{warning.detail}</span>
						</div>
					</div>
				{/each}
			</div>
		{/if}

		{#if visibility.qa}
			<div class="run-section qa-section">
				<div class="section-heading">
					<div>
						<span class="eyebrow">QA</span>
						<h3>{visibility.qa.level} verdict: {visibility.qa.verdict}</h3>
					</div>
					<div class="section-meta">
						<span class="pill" data-tone={visibility.qa.passed ? 'success' : 'warning'}>
							{visibility.qa.passed ? 'tests passed' : 'tests failed'}
						</span>
						{#if visibility.qa.durationMs}
							<span class="pill">{formatDuration(visibility.qa.durationMs)}</span>
						{/if}
					</div>
				</div>
				{#if visibility.qa.summary}
					<p class="section-summary">{clip(visibility.qa.summary, 320)}</p>
				{/if}
				{#if visibility.qa.skippedTests.length > 0}
					<div class="skip-list">
						<span class="skip-title">
							<Icon name="skip-forward" size={13} />
							{visibility.qa.skippedTests.length} skipped live test{visibility.qa.skippedTests.length === 1 ? '' : 's'}
						</span>
						{#each visibility.qa.skippedTests.slice(0, 4) as skipped, index (skipped.name + index)}
							<span class="skip-item">{skipped.name}</span>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		{#if visibility.taskGroups.length > 0}
			<div class="run-section">
				<div class="section-heading">
					<div>
						<span class="eyebrow">Execution</span>
						<h3>{visibility.taskStats.totalGroups} task group{visibility.taskStats.totalGroups === 1 ? '' : 's'}</h3>
					</div>
					<div class="section-meta">
						<span class="pill" data-tone="success">{visibility.taskStats.approvedGroups} approved</span>
						{#if visibility.taskStats.activeGroups > 0}
							<span class="pill" data-tone="active">{visibility.taskStats.activeGroups} active</span>
						{/if}
						{#if visibility.taskStats.orphanedGroups > 0}
							<span class="pill" data-tone="warning">{visibility.taskStats.orphanedGroups} recovered</span>
						{/if}
					</div>
				</div>

				<div class="task-list">
					{#each visibility.taskGroups as group (group.key)}
						<div class="task-group" data-status={group.status}>
							<div class="task-main">
								<span class="req-chip">{shortRequirement(group.requirementId)}</span>
								<span class="task-title">{group.title}</span>
								<span class="task-status" data-status={group.status}>{groupStatusLabel(group.status)}</span>
							</div>
							<div class="attempt-list">
								{#each group.attempts as attempt, index (attempt.taskId)}
									<div class="attempt-row" data-tone={attemptTone(attempt)}>
										<Icon name={attemptIcon(attempt)} size={13} />
										<span>Attempt {index + 1}</span>
										<span class="attempt-stage">{attempt.stage}</span>
										{#if cycleLabel(attempt)}
											<span>{cycleLabel(attempt)}</span>
										{/if}
										{#if attempt.mergeCommit}
											<span class="sha">
												<Icon name="git-commit" size={12} />
												{shortSha(attempt.mergeCommit)}
											</span>
										{/if}
										{#if attempt.updatedAt}
											<span class="attempt-time">{timeLabel(attempt.updatedAt)}</span>
										{/if}
									</div>
								{/each}
							</div>
						</div>
					{/each}
				</div>
			</div>
		{/if}

		{#if displayedLessons.length > 0}
			<div class="run-section lessons-section">
				<div class="section-heading">
					<div>
						<span class="eyebrow">Learning</span>
						<h3>{visibility.lessons.length} lesson{visibility.lessons.length === 1 ? '' : 's'} captured</h3>
					</div>
					<div class="section-meta">
						<span class="pill" data-tone="success">
							{visibility.lessons.filter((lesson) => lesson.positive).length} reinforcing
						</span>
						<span class="pill" data-tone="warning">
							{visibility.lessons.filter((lesson) => !lesson.positive).length} corrective
						</span>
					</div>
				</div>
				<div class="lesson-list">
					{#each displayedLessons as lesson (lesson.id)}
						<div class="lesson-row" data-positive={lesson.positive}>
							<Icon name={lesson.positive ? 'lightbulb' : 'alert-circle'} size={14} />
							<div class="lesson-copy">
								<span>{lesson.summary}</span>
								<small>
									{lesson.relatedTaskTitle ?? lesson.source}
									<span aria-hidden="true">·</span>
									{lesson.futureRunOnly ? 'future-run only' : `injected ${timeLabel(lesson.lastInjectedAt)}`}
								</small>
							</div>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	</section>
{/if}

<style>
	.run-visibility {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin-bottom: var(--space-4);
	}

	.status-band {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-bg-secondary);
	}

	.status-band[data-tone='active'] {
		border-color: var(--color-accent);
		box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-accent) 14%, transparent);
	}

	.status-band[data-tone='success'] {
		border-color: var(--color-success);
	}

	.status-band[data-tone='warning'] {
		border-color: var(--color-warning);
	}

	.status-band[data-tone='danger'] {
		border-color: var(--color-error);
	}

	.status-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		color: var(--color-text-secondary);
		background: var(--color-bg-tertiary);
	}

	.status-band[data-tone='active'] .status-icon {
		color: var(--color-accent);
	}

	.status-band[data-tone='active'] .status-icon :global(svg) {
		animation: spin 1.6s linear infinite;
	}

	.status-band[data-tone='success'] .status-icon {
		color: var(--color-success);
	}

	.status-band[data-tone='warning'] .status-icon {
		color: var(--color-warning);
	}

	.status-band[data-tone='danger'] .status-icon {
		color: var(--color-error);
	}

	.status-copy {
		min-width: 0;
	}

	.eyebrow {
		display: block;
		margin-bottom: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		text-transform: uppercase;
	}

	h2,
	h3,
	p {
		margin: 0;
	}

	h2 {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	h3 {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	p,
	.section-summary {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
	}

	.status-meta,
	.section-meta,
	.metric-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.status-meta {
		justify-content: flex-end;
	}

	.stage-chip,
	.time-chip,
	.pill,
	.req-chip,
	.task-status {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 22px;
		padding: 0 var(--space-2);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		background: var(--color-bg-tertiary);
		white-space: nowrap;
	}

	.metric-row {
		display: grid;
		grid-template-columns: repeat(5, minmax(0, 1fr));
		gap: var(--space-2);
	}

	.metric {
		display: flex;
		min-width: 0;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
	}

	.metric span {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.metric strong {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		line-height: var(--line-height-tight);
		overflow-wrap: anywhere;
	}

	.metric-alert strong {
		color: var(--color-warning);
	}

	.warning-lane,
	.run-section {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-bg-secondary);
		overflow: hidden;
	}

	.warning-row {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		color: var(--color-text-secondary);
		border-bottom: 1px solid var(--color-border);
	}

	.warning-row:last-child {
		border-bottom: none;
	}

	.warning-row[data-tone='warning'] {
		color: var(--color-warning);
	}

	.warning-row[data-tone='danger'] {
		color: var(--color-error);
	}

	.warning-row div {
		display: flex;
		min-width: 0;
		flex-direction: column;
		gap: 2px;
	}

	.warning-row strong {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.warning-row span {
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
	}

	.run-section {
		padding: var(--space-3);
	}

	.section-heading {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}

	.pill[data-tone='success'],
	.task-status[data-status='approved'] {
		color: var(--color-success);
		background: color-mix(in srgb, var(--color-success) 14%, transparent);
	}

	.pill[data-tone='warning'],
	.task-status[data-status='recovered'],
	.task-status[data-status='blocked'] {
		color: var(--color-warning);
		background: color-mix(in srgb, var(--color-warning) 14%, transparent);
	}

	.pill[data-tone='active'],
	.task-status[data-status='active'] {
		color: var(--color-accent);
		background: color-mix(in srgb, var(--color-accent) 14%, transparent);
	}

	.skip-list,
	.task-list,
	.lesson-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.skip-title,
	.skip-item {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	.skip-title {
		color: var(--color-warning);
		font-weight: var(--font-weight-medium);
	}

	.task-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		border-top: 1px solid var(--color-border);
	}

	.task-group:first-child {
		border-top: none;
		padding-top: 0;
	}

	.task-group:last-child {
		padding-bottom: 0;
	}

	.task-main {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: var(--space-2);
	}

	.task-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		line-height: var(--line-height-normal);
		overflow-wrap: anywhere;
	}

	.attempt-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding-left: var(--space-4);
	}

	.attempt-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 22px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.attempt-row[data-tone='success'] {
		color: var(--color-success);
	}

	.attempt-row[data-tone='warning'] {
		color: var(--color-warning);
	}

	.attempt-row[data-tone='danger'] {
		color: var(--color-error);
	}

	.attempt-stage,
	.sha,
	.attempt-time {
		color: var(--color-text-secondary);
	}

	.sha {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		font-family: var(--font-family-mono);
	}

	.lesson-row {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: var(--space-2);
		color: var(--color-warning);
	}

	.lesson-row[data-positive='true'] {
		color: var(--color-success);
	}

	.lesson-copy {
		display: flex;
		min-width: 0;
		flex-direction: column;
		gap: 2px;
	}

	.lesson-copy span {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: var(--line-height-normal);
	}

	.lesson-copy small {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	@media (max-width: 720px) {
		.status-band,
		.task-main {
			grid-template-columns: auto minmax(0, 1fr);
		}

		.status-meta,
		.task-status {
			grid-column: 1 / -1;
			justify-content: flex-start;
		}

		.metric-row {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}

		.section-heading {
			flex-direction: column;
		}
	}
</style>
