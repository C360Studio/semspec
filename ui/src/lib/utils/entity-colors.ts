/**
 * Entity type color mapping for semspec knowledge graph visualization.
 *
 * Maps the six semspec entity types to distinct, accessible colors.
 * Used by both the graph visualization (Sigma.js nodes) and the filter UI.
 *
 * Color palette is chosen for distinguishability in WebGL rendering on dark backgrounds.
 */

import type { EntityType } from '$lib/types';

// =============================================================================
// Color Map
// =============================================================================

/**
 * Hex color per semspec entity type.
 * Also includes 'unknown' as a fallback for unrecognized types.
 */
export const ENTITY_COLORS: Record<EntityType | 'unknown', string> = {
	code: '#3b82f6', // blue
	spec: '#a855f7', // purple
	task: '#22c55e', // green
	loop: '#f97316', // orange
	proposal: '#eab308', // yellow
	activity: '#6b7280', // gray
	unknown: '#4b5563' // dark gray
};

/**
 * Return the hex color for a given semspec entity type string.
 * Falls back to the 'unknown' color for unrecognized types.
 */
export function getEntityColor(type: string): string {
	return ENTITY_COLORS[type as EntityType] ?? ENTITY_COLORS.unknown;
}

/**
 * Return a muted edge color derived from the predicate name.
 * Uses a simple hash to pick from a palette of muted colors so related
 * predicates stay visually distinct without overwhelming the nodes.
 */
export function getPredicateColor(predicate: string): string {
	const PREDICATE_COLORS = [
		'#64748b',
		'#475569',
		'#94a3b8',
		'#7c8c9a',
		'#8097a8',
		'#5f7282'
	];
	let hash = 0;
	for (let i = 0; i < predicate.length; i++) {
		hash = (hash * 31 + predicate.charCodeAt(i)) >>> 0;
	}
	return PREDICATE_COLORS[hash % PREDICATE_COLORS.length];
}

/**
 * Return a CSS opacity value (0.3–1.0) scaled from a confidence score (0–1).
 * Low-confidence properties render translucent to signal uncertainty.
 */
export function getConfidenceOpacity(confidence: number): number {
	return Math.max(0.3, Math.min(1, 0.3 + confidence * 0.7));
}
