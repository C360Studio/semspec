<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { ReviewFinding, SynthesisStats } from '$lib/types/review';
	import { getSeverityLabel, getSeverityClass, getReviewerLabel } from '$lib/types/review';

	interface Props {
		/** List of findings to display */
		findings: ReviewFinding[];
		/** Optional statistics */
		stats?: SynthesisStats;
		/** Whether to show the header */
		showHeader?: boolean;
		/** Maximum items to show before collapsing */
		maxVisible?: number;
	}

	let { findings, stats, showHeader = true, maxVisible = 10 }: Props = $props();

	let expanded = $state(false);

	const visibleFindings = $derived(
		expanded || findings.length <= maxVisible ? findings : findings.slice(0, maxVisible)
	);

	const hasMore = $derived(findings.length > maxVisible);
	const hiddenCount = $derived(findings.length - maxVisible);
</script>

<div class="findings-list">
	{#if showHeader && stats}
		<div class="findings-header">
			<h3 class="findings-title">
				<Icon name="alert-circle" size={16} />
				Findings ({stats.total_findings})
			</h3>
			<div class="severity-summary">
				{#if stats.by_severity.critical}
					<span class="severity-badge error">
						{stats.by_severity.critical} critical
					</span>
				{/if}
				{#if stats.by_severity.high}
					<span class="severity-badge warning">
						{stats.by_severity.high} high
					</span>
				{/if}
				{#if stats.by_severity.medium}
					<span class="severity-badge info">
						{stats.by_severity.medium} medium
					</span>
				{/if}
				{#if stats.by_severity.low}
					<span class="severity-badge neutral">
						{stats.by_severity.low} low
					</span>
				{/if}
			</div>
		</div>
	{/if}

	{#if findings.length === 0}
		<div class="empty-state">
			<Icon name="check-circle" size={24} />
			<p>No findings reported</p>
		</div>
	{:else}
		<div class="findings-table">
			<table>
				<thead>
					<tr>
						<th>Severity</th>
						<th>Reviewer</th>
						<th>Location</th>
						<th>Issue</th>
					</tr>
				</thead>
				<tbody>
					{#each visibleFindings as finding}
						<tr class="finding-row">
							<td>
								<span class="severity-badge {getSeverityClass(finding.severity)}">
									{getSeverityLabel(finding.severity)}
								</span>
							</td>
							<td class="reviewer-cell">
								{getReviewerLabel(finding.role)}
								{#if finding.cwe}
									<span class="cwe-tag">{finding.cwe}</span>
								{/if}
							</td>
							<td class="location-cell">
								{#if finding.file}
									<code class="file-path">{finding.file}</code>
									{#if finding.line}
										<span class="line-number">:{finding.line}</span>
									{/if}
								{:else}
									<span class="no-location">-</span>
								{/if}
							</td>
							<td class="issue-cell">
								<p class="issue-text">{finding.issue}</p>
								{#if finding.suggestion}
									<p class="suggestion-text">
										<Icon name="info" size={12} />
										{finding.suggestion}
									</p>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		{#if hasMore}
			<button class="show-more-btn" onclick={() => (expanded = !expanded)}>
				<Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={14} />
				{expanded ? 'Show less' : `Show ${hiddenCount} more`}
			</button>
		{/if}
	{/if}
</div>

<style>
	.findings-list {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.findings-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
	}

	.findings-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.severity-summary {
		display: flex;
		gap: var(--space-2);
	}

	.severity-badge {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-2);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-full);
		text-transform: capitalize;
	}

	.severity-badge.error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.severity-badge.warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.severity-badge.info {
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.severity-badge.neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-success);
	}

	.empty-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.findings-table {
		overflow-x: auto;
	}

	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--font-size-sm);
	}

	thead {
		background: var(--color-bg-tertiary);
	}

	th {
		padding: var(--space-2) var(--space-3);
		text-align: left;
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
		border-bottom: 1px solid var(--color-border);
		white-space: nowrap;
	}

	td {
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		vertical-align: top;
	}

	.finding-row:last-child td {
		border-bottom: none;
	}

	.reviewer-cell {
		white-space: nowrap;
	}

	.cwe-tag {
		display: inline-block;
		margin-left: var(--space-1);
		padding: 1px var(--space-1);
		font-size: 9px;
		font-family: var(--font-family-mono);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
	}

	.location-cell {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.file-path {
		color: var(--color-accent);
		background: none;
		padding: 0;
	}

	.line-number {
		color: var(--color-text-muted);
	}

	.no-location {
		color: var(--color-text-muted);
	}

	.issue-cell {
		max-width: 400px;
	}

	.issue-text {
		margin: 0;
		color: var(--color-text-primary);
		line-height: var(--line-height-normal);
	}

	.suggestion-text {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1);
		margin: var(--space-2) 0 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
	}

	.suggestion-text :global(svg) {
		flex-shrink: 0;
		margin-top: 2px;
	}

	.show-more-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		width: 100%;
		padding: var(--space-2);
		background: var(--color-bg-tertiary);
		border: none;
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.show-more-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}
</style>
