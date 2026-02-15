/**
 * Types for the agent activity timeline visualization.
 * Represents time-based segments of agent activity.
 */

// =============================================================================
// Segment Types
// =============================================================================

/** State of a timeline segment */
export type SegmentState = 'active' | 'waiting' | 'blocked' | 'complete' | 'failed';

/**
 * A time segment representing agent activity
 */
export interface TimelineSegment {
	/** Unique segment ID */
	id: string;
	/** Loop ID this segment belongs to */
	loopId: string;
	/** Start time (ISO string) */
	startTime: string;
	/** End time (ISO string), undefined if still active */
	endTime?: string;
	/** Segment state */
	state: SegmentState;
	/** Number of iterations at this point */
	iterations?: number;
	/** Max iterations */
	maxIterations?: number;
	/** Current task being worked on */
	taskId?: string;
	/** Activity type (for granular segments) */
	activityType?: string;
}

// =============================================================================
// Track Types
// =============================================================================

/**
 * A timeline track for a single agent/role
 */
export interface TimelineTrack {
	/** Track identifier (role name) */
	id: string;
	/** Display label */
	label: string;
	/** Short label for compact view */
	shortLabel: string;
	/** Agent role */
	role: string;
	/** All segments for this track */
	segments: TimelineSegment[];
	/** Current state of the track */
	currentState: SegmentState | 'idle';
	/** Loop ID if currently active */
	activeLoopId?: string;
}

// =============================================================================
// Timeline State
// =============================================================================

/**
 * Overall timeline state
 */
export interface TimelineState {
	/** All tracks in the timeline */
	tracks: TimelineTrack[];
	/** Earliest time in the timeline */
	startTime: string;
	/** Latest time in the timeline */
	endTime: string;
	/** Time span in milliseconds */
	duration: number;
	/** Whether the timeline has any active segments */
	hasActive: boolean;
}

// =============================================================================
// Timeline Configuration
// =============================================================================

/**
 * Timeline display configuration
 */
export interface TimelineConfig {
	/** Minimum time span to show (ms) */
	minDuration: number;
	/** Pixel width per millisecond */
	pixelsPerMs: number;
	/** Show time labels */
	showTimeLabels: boolean;
	/** Time label interval (ms) */
	timeLabelInterval: number;
	/** Auto-scroll to follow active segments */
	autoScroll: boolean;
}

/** Default timeline configuration */
export const DEFAULT_TIMELINE_CONFIG: TimelineConfig = {
	minDuration: 5 * 60 * 1000, // 5 minutes minimum
	pixelsPerMs: 0.01, // 10px per second
	showTimeLabels: true,
	timeLabelInterval: 5 * 60 * 1000, // 5 minute intervals
	autoScroll: true
};

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Get CSS class for segment state
 */
export function getSegmentClass(state: SegmentState): string {
	switch (state) {
		case 'active':
			return 'segment-active';
		case 'waiting':
			return 'segment-waiting';
		case 'blocked':
			return 'segment-blocked';
		case 'complete':
			return 'segment-complete';
		case 'failed':
			return 'segment-failed';
	}
}

/**
 * Get color for segment state
 */
export function getSegmentColor(state: SegmentState): string {
	switch (state) {
		case 'active':
			return 'var(--color-info)';
		case 'waiting':
			return 'var(--color-text-muted)';
		case 'blocked':
			return 'var(--color-warning)';
		case 'complete':
			return 'var(--color-success)';
		case 'failed':
			return 'var(--color-error)';
	}
}

/**
 * Format duration for display
 */
export function formatDuration(ms: number): string {
	const seconds = Math.floor(ms / 1000);
	const minutes = Math.floor(seconds / 60);
	const hours = Math.floor(minutes / 60);

	if (hours > 0) {
		return `${hours}h ${minutes % 60}m`;
	}
	if (minutes > 0) {
		return `${minutes}m ${seconds % 60}s`;
	}
	return `${seconds}s`;
}

/**
 * Format time for labels
 */
export function formatTimeLabel(date: Date): string {
	return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

/**
 * Calculate segment width as percentage of total duration
 */
export function getSegmentWidth(segment: TimelineSegment, totalDuration: number): number {
	const start = new Date(segment.startTime).getTime();
	const end = segment.endTime ? new Date(segment.endTime).getTime() : Date.now();
	const duration = end - start;
	return (duration / totalDuration) * 100;
}

/**
 * Calculate segment offset as percentage of total duration
 */
export function getSegmentOffset(
	segment: TimelineSegment,
	timelineStart: string,
	totalDuration: number
): number {
	const start = new Date(segment.startTime).getTime();
	const timelineStartMs = new Date(timelineStart).getTime();
	const offset = start - timelineStartMs;
	return (offset / totalDuration) * 100;
}

/**
 * Map loop state to segment state
 */
export function loopStateToSegmentState(loopState: string): SegmentState {
	switch (loopState) {
		case 'pending':
		case 'exploring':
		case 'executing':
			return 'active';
		case 'paused':
			return 'waiting';
		case 'blocked':
			return 'blocked';
		case 'complete':
		case 'success':
			return 'complete';
		case 'failed':
		case 'cancelled':
			return 'failed';
		default:
			return 'waiting';
	}
}

/**
 * Get role display label
 */
export function getRoleLabel(role: string): string {
	const labels: Record<string, string> = {
		planner: 'Planner',
		'planner-writer': 'Planner',
		'task-generator': 'Task Gen',
		'task-generator-writer': 'Task Gen',
		developer: 'Developer',
		'developer-writer': 'Developer',
		spec_reviewer: 'Spec Review',
		sop_reviewer: 'SOP Review',
		style_reviewer: 'Style Review',
		security_reviewer: 'Security Review'
	};
	return labels[role] || role;
}

/**
 * Get role short label
 */
export function getRoleShortLabel(role: string): string {
	const labels: Record<string, string> = {
		explorer: 'Exp',
		'explorer-writer': 'Exp',
		planner: 'Plan',
		'planner-writer': 'Plan',
		'task-generator': 'Tasks',
		'task-generator-writer': 'Tasks',
		developer: 'Dev',
		'developer-writer': 'Dev',
		spec_reviewer: 'Spec',
		sop_reviewer: 'SOP',
		style_reviewer: 'Style',
		security_reviewer: 'Sec'
	};
	return labels[role] || role.slice(0, 4);
}
