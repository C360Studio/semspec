/**
 * Timeline store for agent activity visualization.
 * Derives timeline data from loops and activity events.
 */

import type { Loop, ActivityEvent } from '$lib/types';
import type {
	TimelineState,
	TimelineTrack,
	TimelineSegment,
	TimelineConfig,
	SegmentState
} from '$lib/types/timeline';
import {
	DEFAULT_TIMELINE_CONFIG,
	loopStateToSegmentState,
	getRoleLabel,
	getRoleShortLabel
} from '$lib/types/timeline';

/** Minimal loop interface for timeline */
interface LoopLike {
	loop_id: string;
	role?: string;
	state: string;
	created_at?: string;
	iterations?: number;
	max_iterations?: number;
	workflow_slug?: string;
	current_task_id?: string;
}

class TimelineStore {
	// State
	tracks = $state<TimelineTrack[]>([]);
	startTime = $state<string>(new Date().toISOString());
	endTime = $state<string>(new Date().toISOString());
	config = $state<TimelineConfig>(DEFAULT_TIMELINE_CONFIG);
	loading = $state(false);

	// Computed
	get duration(): number {
		const start = new Date(this.startTime).getTime();
		const end = new Date(this.endTime).getTime();
		return Math.max(end - start, this.config.minDuration);
	}

	get hasActive(): boolean {
		return this.tracks.some((t) => t.currentState === 'active');
	}

	get state(): TimelineState {
		return {
			tracks: this.tracks,
			startTime: this.startTime,
			endTime: this.endTime,
			duration: this.duration,
			hasActive: this.hasActive
		};
	}

	/**
	 * Build timeline from loops
	 */
	buildFromLoops(loops: LoopLike[]): void {
		if (loops.length === 0) {
			this.tracks = [];
			return;
		}

		// Group loops by role
		const loopsByRole = new Map<string, LoopLike[]>();
		for (const loop of loops) {
			const role = loop.role || 'unknown';
			const existing = loopsByRole.get(role) || [];
			existing.push(loop);
			loopsByRole.set(role, existing);
		}

		// Calculate time bounds
		let minTime = Infinity;
		let maxTime = -Infinity;

		for (const loop of loops) {
			if (loop.created_at) {
				const time = new Date(loop.created_at).getTime();
				minTime = Math.min(minTime, time);
				maxTime = Math.max(maxTime, time);
			}
		}

		// If no valid times, use current time
		if (minTime === Infinity) {
			minTime = Date.now() - this.config.minDuration;
			maxTime = Date.now();
		}

		// Extend end time to now if there are active loops
		const hasActiveLoop = loops.some((l) =>
			['pending', 'exploring', 'executing'].includes(l.state)
		);
		if (hasActiveLoop) {
			maxTime = Date.now();
		}

		this.startTime = new Date(minTime).toISOString();
		this.endTime = new Date(maxTime).toISOString();

		// Build tracks
		const tracks: TimelineTrack[] = [];
		const roleOrder = [
			'explorer',
			'explorer-writer',
			'planner',
			'planner-writer',
			'task-generator',
			'task-generator-writer',
			'developer',
			'developer-writer',
			'spec_reviewer',
			'sop_reviewer',
			'style_reviewer',
			'security_reviewer'
		];

		// Sort roles by predefined order
		const sortedRoles = [...loopsByRole.keys()].sort((a, b) => {
			const aIndex = roleOrder.indexOf(a);
			const bIndex = roleOrder.indexOf(b);
			if (aIndex === -1 && bIndex === -1) return a.localeCompare(b);
			if (aIndex === -1) return 1;
			if (bIndex === -1) return -1;
			return aIndex - bIndex;
		});

		for (const role of sortedRoles) {
			const roleLoops = loopsByRole.get(role) || [];
			const track = this.buildTrack(role, roleLoops);
			tracks.push(track);
		}

		this.tracks = tracks;
	}

	/**
	 * Build a single track from loops
	 */
	private buildTrack(role: string, loops: LoopLike[]): TimelineTrack {
		const segments: TimelineSegment[] = [];
		let currentState: SegmentState | 'idle' = 'idle';
		let activeLoopId: string | undefined;

		// Sort loops by creation time
		const sortedLoops = [...loops].sort((a, b) => {
			if (!a.created_at || !b.created_at) return 0;
			return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
		});

		for (const loop of sortedLoops) {
			const segmentState = loopStateToSegmentState(loop.state);

			// Create segment for this loop
			const segment: TimelineSegment = {
				id: `${loop.loop_id}-main`,
				loopId: loop.loop_id,
				startTime: loop.created_at || new Date().toISOString(),
				state: segmentState,
				iterations: loop.iterations,
				maxIterations: loop.max_iterations,
				taskId: loop.current_task_id
			};

			// If not active, estimate end time based on state
			if (!['active', 'waiting', 'blocked'].includes(segmentState)) {
				// For completed/failed, estimate end time
				// In a real implementation, this would come from actual data
				const startMs = new Date(segment.startTime).getTime();
				const estimatedDuration = (loop.iterations || 1) * 30000; // 30s per iteration estimate
				segment.endTime = new Date(startMs + estimatedDuration).toISOString();
			}

			segments.push(segment);

			// Track current state (most recent active loop wins)
			if (['active', 'waiting', 'blocked'].includes(segmentState)) {
				currentState = segmentState;
				activeLoopId = loop.loop_id;
			}
		}

		// If no active segments but has complete segments, mark as complete
		if (currentState === 'idle' && segments.length > 0) {
			const lastSegment = segments[segments.length - 1];
			if (lastSegment.state === 'complete') {
				currentState = 'complete';
			} else if (lastSegment.state === 'failed') {
				currentState = 'failed';
			}
		}

		return {
			id: role,
			label: getRoleLabel(role),
			shortLabel: getRoleShortLabel(role),
			role,
			segments,
			currentState,
			activeLoopId
		};
	}

	/**
	 * Update timeline with new activity event
	 */
	addActivity(event: ActivityEvent): void {
		const track = this.tracks.find((t) =>
			t.segments.some((s) => s.loopId === event.loop_id)
		);

		if (!track) return;

		// Find or create segment for this loop
		let segment = track.segments.find((s) => s.loopId === event.loop_id);

		if (!segment) {
			// Create new segment
			segment = {
				id: `${event.loop_id}-${event.type}`,
				loopId: event.loop_id,
				startTime: event.timestamp,
				state: 'active'
			};
			track.segments.push(segment);
		}

		// Update end time if this event has completed the segment
		if (event.type === 'loop_completed') {
			segment.endTime = event.timestamp;
			// Parse data to get completion state if available
			if (event.data) {
				try {
					const data = JSON.parse(atob(event.data));
					if (data.state) {
						segment.state = loopStateToSegmentState(data.state);
					}
				} catch {
					// Ignore parse errors
				}
			}
		}

		// Extend timeline end time
		const eventTime = new Date(event.timestamp).getTime();
		const currentEnd = new Date(this.endTime).getTime();
		if (eventTime > currentEnd) {
			this.endTime = event.timestamp;
		}
	}

	/**
	 * Update configuration
	 */
	setConfig(config: Partial<TimelineConfig>): void {
		this.config = { ...this.config, ...config };
	}

	/**
	 * Clear all timeline data
	 */
	clear(): void {
		this.tracks = [];
		this.startTime = new Date().toISOString();
		this.endTime = new Date().toISOString();
	}

	/**
	 * Get time labels for the timeline axis
	 */
	getTimeLabels(): { time: Date; offset: number }[] {
		const labels: { time: Date; offset: number }[] = [];
		const startMs = new Date(this.startTime).getTime();
		const endMs = new Date(this.endTime).getTime();
		const interval = this.config.timeLabelInterval;

		// Round start to nearest interval
		const firstLabel = Math.ceil(startMs / interval) * interval;

		for (let time = firstLabel; time <= endMs; time += interval) {
			const offset = ((time - startMs) / this.duration) * 100;
			labels.push({ time: new Date(time), offset });
		}

		return labels;
	}
}

export const timelineStore = new TimelineStore();
