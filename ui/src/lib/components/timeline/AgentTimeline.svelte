<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import TimelineTrack from './TimelineTrack.svelte';
	import { timelineStore } from '$lib/stores/timeline.svelte';
	import type { TimelineSegment } from '$lib/types/timeline';
	import { formatTimeLabel, formatDuration } from '$lib/types/timeline';

	interface LoopLike {
		loop_id: string;
		role?: string;
		state: string;
		created_at?: string;
		iterations?: number;
		max_iterations?: number;
	}

	interface Props {
		/** Loops to visualize */
		loops?: LoopLike[];
		/** Compact mode */
		compact?: boolean;
		/** Show time axis */
		showTimeAxis?: boolean;
		/** Show legend */
		showLegend?: boolean;
		/** Click handler for segments */
		onSegmentClick?: (segment: TimelineSegment) => void;
	}

	let {
		loops = [],
		compact = false,
		showTimeAxis = true,
		showLegend = true,
		onSegmentClick
	}: Props = $props();

	// Build timeline when loops change
	$effect(() => {
		if (loops.length > 0) {
			timelineStore.buildFromLoops(loops);
		}
	});

	// Get time labels for the axis
	const timeLabels = $derived(timelineStore.getTimeLabels());

	// Selected segment state
	let selectedSegment = $state<TimelineSegment | null>(null);

	function handleSegmentClick(segment: TimelineSegment) {
		selectedSegment = segment;
		onSegmentClick?.(segment);
	}

	function clearSelection() {
		selectedSegment = null;
	}
</script>

<div class="agent-timeline" class:compact>
	<div class="timeline-header">
		<h3 class="timeline-title">
			<Icon name="activity" size={compact ? 14 : 16} />
			Agent Timeline
		</h3>

		{#if timelineStore.hasActive}
			<span class="live-indicator">
				<span class="live-dot"></span>
				Live
			</span>
		{/if}

		<span class="duration-badge">
			{formatDuration(timelineStore.duration)}
		</span>
	</div>

	{#if showTimeAxis && timeLabels.length > 0}
		<div class="time-axis">
			{#each timeLabels as label}
				<div class="time-label" style="left: {label.offset}%">
					{formatTimeLabel(label.time)}
				</div>
			{/each}
		</div>
	{/if}

	<div class="tracks-container">
		{#if timelineStore.tracks.length === 0}
			<div class="empty-state">
				<Icon name="clock" size={32} />
				<p>No agent activity to display</p>
				<span class="empty-hint">Activity will appear here when agents start working</span>
			</div>
		{:else}
			{#each timelineStore.tracks as track}
				<TimelineTrack
					{track}
					timelineStart={timelineStore.startTime}
					totalDuration={timelineStore.duration}
					{compact}
					onSegmentClick={handleSegmentClick}
				/>
			{/each}
		{/if}
	</div>

	{#if showLegend}
		<div class="timeline-legend">
			<div class="legend-item">
				<span class="legend-color active"></span>
				<span class="legend-label">Active</span>
			</div>
			<div class="legend-item">
				<span class="legend-color complete"></span>
				<span class="legend-label">Complete</span>
			</div>
			<div class="legend-item">
				<span class="legend-color waiting"></span>
				<span class="legend-label">Waiting</span>
			</div>
			<div class="legend-item">
				<span class="legend-color blocked"></span>
				<span class="legend-label">Blocked</span>
			</div>
			<div class="legend-item">
				<span class="legend-color failed"></span>
				<span class="legend-label">Failed</span>
			</div>
		</div>
	{/if}

	{#if selectedSegment}
		<div class="segment-details">
			<button class="close-btn" onclick={clearSelection}>
				<Icon name="x" size={14} />
			</button>
			<div class="detail-row">
				<span class="detail-label">Loop ID:</span>
				<span class="detail-value mono">{selectedSegment.loopId.slice(0, 8)}...</span>
			</div>
			<div class="detail-row">
				<span class="detail-label">State:</span>
				<span class="detail-value badge-{selectedSegment.state}">{selectedSegment.state}</span>
			</div>
			{#if selectedSegment.iterations !== undefined}
				<div class="detail-row">
					<span class="detail-label">Progress:</span>
					<span class="detail-value">{selectedSegment.iterations}/{selectedSegment.maxIterations}</span>
				</div>
			{/if}
			<div class="detail-row">
				<span class="detail-label">Started:</span>
				<span class="detail-value">{new Date(selectedSegment.startTime).toLocaleTimeString()}</span>
			</div>
			{#if selectedSegment.endTime}
				<div class="detail-row">
					<span class="detail-label">Duration:</span>
					<span class="detail-value">
						{formatDuration(
							new Date(selectedSegment.endTime).getTime() -
								new Date(selectedSegment.startTime).getTime()
						)}
					</span>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.agent-timeline {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
	}

	.agent-timeline.compact {
		padding: var(--space-3);
		gap: var(--space-2);
	}

	.timeline-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.timeline-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.compact .timeline-title {
		font-size: var(--font-size-sm);
	}

	.live-indicator {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-success);
	}

	.live-dot {
		width: 8px;
		height: 8px;
		background: var(--color-success);
		border-radius: var(--radius-full);
		animation: pulse 1.5s ease-in-out infinite;
	}

	.duration-badge {
		margin-left: auto;
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
	}

	.time-axis {
		position: relative;
		height: 20px;
		margin-left: 120px;
		border-bottom: 1px solid var(--color-border);
	}

	.compact .time-axis {
		margin-left: 80px;
		height: 16px;
	}

	.time-label {
		position: absolute;
		transform: translateX(-50%);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		white-space: nowrap;
	}

	.tracks-container {
		display: flex;
		flex-direction: column;
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

	.empty-state p {
		margin: 0;
		color: var(--color-text-secondary);
	}

	.empty-hint {
		font-size: var(--font-size-sm);
	}

	.timeline-legend {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4);
		padding-top: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	.legend-item {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.legend-color {
		width: 12px;
		height: 12px;
		border-radius: var(--radius-sm);
	}

	.legend-color.active {
		background: var(--color-info);
	}

	.legend-color.complete {
		background: var(--color-success);
	}

	.legend-color.waiting {
		background: var(--color-text-muted);
		opacity: 0.5;
	}

	.legend-color.blocked {
		background: var(--color-warning);
	}

	.legend-color.failed {
		background: var(--color-error);
	}

	.legend-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.segment-details {
		position: relative;
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.close-btn {
		position: absolute;
		top: var(--space-2);
		right: var(--space-2);
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
	}

	.close-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.detail-row {
		display: flex;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
	}

	.detail-row + .detail-row {
		margin-top: var(--space-1);
	}

	.detail-label {
		color: var(--color-text-muted);
		min-width: 80px;
	}

	.detail-value {
		color: var(--color-text-primary);
	}

	.detail-value.mono {
		font-family: var(--font-family-mono);
	}

	.badge-active {
		color: var(--color-info);
	}

	.badge-complete {
		color: var(--color-success);
	}

	.badge-waiting {
		color: var(--color-text-muted);
	}

	.badge-blocked {
		color: var(--color-warning);
	}

	.badge-failed {
		color: var(--color-error);
	}

	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.5;
		}
	}
</style>
