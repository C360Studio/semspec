<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { TimelineTrack, TimelineSegment } from '$lib/types/timeline';
	import {
		getSegmentWidth,
		getSegmentOffset,
		getSegmentClass,
		formatDuration
	} from '$lib/types/timeline';

	interface Props {
		/** Track data */
		track: TimelineTrack;
		/** Timeline start time */
		timelineStart: string;
		/** Total timeline duration in ms */
		totalDuration: number;
		/** Compact mode */
		compact?: boolean;
		/** Click handler for segments */
		onSegmentClick?: (segment: TimelineSegment) => void;
	}

	let {
		track,
		timelineStart,
		totalDuration,
		compact = false,
		onSegmentClick
	}: Props = $props();

	// Get icon for track state
	function getTrackIcon(state: string): string {
		switch (state) {
			case 'active':
				return 'loader';
			case 'waiting':
				return 'clock';
			case 'blocked':
				return 'alert-triangle';
			case 'complete':
				return 'check';
			case 'failed':
				return 'x';
			default:
				return 'circle';
		}
	}

	// Calculate segment position and dimensions
	function getSegmentStyle(segment: TimelineSegment): string {
		const width = getSegmentWidth(segment, totalDuration);
		const offset = getSegmentOffset(segment, timelineStart, totalDuration);
		return `left: ${offset}%; width: ${Math.max(width, 0.5)}%;`;
	}

	// Calculate segment duration
	function getSegmentDuration(segment: TimelineSegment): number {
		const start = new Date(segment.startTime).getTime();
		const end = segment.endTime ? new Date(segment.endTime).getTime() : Date.now();
		return end - start;
	}
</script>

<div class="timeline-track" class:compact class:active={track.currentState === 'active'}>
	<div class="track-label">
		<div class="label-icon" data-state={track.currentState}>
			<Icon
				name={getTrackIcon(track.currentState)}
				size={compact ? 12 : 14}
				class={track.currentState === 'active' ? 'spin' : ''}
			/>
		</div>
		<span class="label-text">{compact ? track.shortLabel : track.label}</span>
	</div>

	<div class="track-bar">
		{#each track.segments as segment}
			<button
				class="segment {getSegmentClass(segment.state)}"
				class:has-progress={segment.iterations !== undefined}
				style={getSegmentStyle(segment)}
				onclick={() => onSegmentClick?.(segment)}
				title="{track.label}: {formatDuration(getSegmentDuration(segment))}"
			>
				{#if segment.iterations !== undefined && segment.maxIterations}
					<span class="segment-progress">
						{segment.iterations}/{segment.maxIterations}
					</span>
				{/if}
			</button>
		{/each}

		{#if track.segments.length === 0}
			<div class="empty-track">
				<span class="empty-label">No activity</span>
			</div>
		{/if}
	</div>
</div>

<style>
	.timeline-track {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2) 0;
	}

	.timeline-track.compact {
		padding: var(--space-1) 0;
		gap: var(--space-2);
	}

	.track-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 120px;
		flex-shrink: 0;
	}

	.compact .track-label {
		min-width: 80px;
		gap: var(--space-1);
	}

	.label-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		border-radius: var(--radius-full);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.compact .label-icon {
		width: 20px;
		height: 20px;
	}

	.label-icon[data-state='active'] {
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.label-icon[data-state='waiting'] {
		background: var(--color-bg-elevated);
		color: var(--color-text-muted);
	}

	.label-icon[data-state='blocked'] {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.label-icon[data-state='complete'] {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.label-icon[data-state='failed'] {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.label-text {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		white-space: nowrap;
	}

	.compact .label-text {
		font-size: var(--font-size-xs);
	}

	.track-bar {
		flex: 1;
		position: relative;
		height: 24px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.compact .track-bar {
		height: 18px;
	}

	.segment {
		position: absolute;
		top: 2px;
		bottom: 2px;
		border-radius: var(--radius-sm);
		border: none;
		cursor: pointer;
		transition: all var(--transition-fast);
		display: flex;
		align-items: center;
		justify-content: center;
		min-width: 4px;
	}

	.segment:hover {
		filter: brightness(1.1);
		z-index: 1;
	}

	.segment-active {
		background: var(--color-info);
	}

	.segment-waiting {
		background: var(--color-text-muted);
		opacity: 0.5;
	}

	.segment-blocked {
		background: var(--color-warning);
	}

	.segment-complete {
		background: var(--color-success);
	}

	.segment-failed {
		background: var(--color-error);
	}

	.segment-progress {
		font-size: 9px;
		font-family: var(--font-family-mono);
		color: white;
		white-space: nowrap;
		padding: 0 4px;
		text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
	}

	.compact .segment-progress {
		font-size: 8px;
	}

	.empty-track {
		display: flex;
		align-items: center;
		justify-content: center;
		height: 100%;
	}

	.empty-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-style: italic;
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
