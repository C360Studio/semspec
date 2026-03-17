/**
 * Entity Color Mapping for Graph Visualization
 *
 * Maps semspec entity types to distinct colors for the graph visualization.
 * Entity types are derived from the first dot-segment of the entity ID.
 *
 * Includes both semspec domain types and semsource knowledge graph types.
 */

import type { EntityIdParts } from '$lib/api/graph-types';

// =============================================================================
// Semspec Entity Type Colors
// =============================================================================

/**
 * Color mapping for entity types in the graph visualization.
 * Keys are the first segment of the entity ID (e.g. "code", "spec").
 */
export const ENTITY_TYPE_COLORS: Record<string, string> = {
  // Semspec domain types
  code: '#3b82f6',      // blue — source code entities
  spec: '#a855f7',      // purple — requirements and specifications
  task: '#22c55e',      // green — work tasks
  loop: '#f97316',      // orange — agent loops
  proposal: '#eab308',  // yellow — change proposals
  activity: '#6b7280',  // gray — activity records
  source: '#06b6d4',    // cyan — source documents (semsource)
  agent: '#ec4899',     // pink — agent entities
  semspec: '#8b5cf6',   // violet — plan and semspec entities

  // Fallback for unknown entity types
  unknown: '#4b5563',   // dark gray
};

// Keep ENTITY_COLORS as an alias for backward compatibility with any code
// that references the old export name.
export const ENTITY_COLORS = ENTITY_TYPE_COLORS;

/**
 * Get the visualization color for a semspec entity type string.
 * Returns dark gray for entity types not in the palette.
 */
export function getEntityTypeColor(type: string | undefined): string {
  if (!type) return ENTITY_TYPE_COLORS.unknown;
  return ENTITY_TYPE_COLORS[type.toLowerCase()] ?? ENTITY_TYPE_COLORS.unknown;
}

// =============================================================================
// Predicate Colors (Relationship edge types)
// =============================================================================

/**
 * Color mapping for relationship predicates by domain prefix.
 * Predicates use dotted notation: domain.category.property
 * Color is derived from the first segment (domain).
 */
export const PREDICATE_COLORS: Record<string, string> = {
  // Semspec predicate domains
  code: '#3b82f6',      // blue — code relationships
  spec: '#a855f7',      // purple — spec/requirement relationships
  dc: '#6b7280',        // gray — Dublin Core metadata
  source: '#06b6d4',    // cyan — source document relationships
  semspec: '#8b5cf6',   // violet — semspec plan relationships
  prov: '#f97316',      // orange — provenance
  agent: '#ec4899',     // pink — agent relationships

  // Category-level colors (for predicates without a domain match)
  lifecycle: '#a855f7',
  progression: '#22d3ee',
  formation: '#22c55e',
  membership: '#eab308',
  review: '#ef4444',
  data: '#64748b',
  state: '#64748b',
  content: '#3b82f6',
  ast: '#14b8a6',
  metadata: '#f59e0b',
  identity: '#94a3b8',
  section: '#f59e0b',
  import: '#8b5cf6',

  // Generic fallback
  default: '#6b7280',
};

/**
 * Get color for a relationship predicate.
 * Extracts the domain (first part) from dotted predicate notation.
 * Falls back to category (second part) if domain has no color entry.
 */
export function getPredicateColor(predicate: string): string {
  const parts = predicate.split('.');
  // Try domain first (first part): "dc.terms.title" → "dc"
  const domain = parts[0] ?? '';
  if (PREDICATE_COLORS[domain.toLowerCase()]) {
    return PREDICATE_COLORS[domain.toLowerCase()];
  }
  // Fall back to category (second part): "quest.lifecycle.claimed" → "lifecycle"
  const category = parts[1] ?? '';
  return PREDICATE_COLORS[category.toLowerCase()] ?? PREDICATE_COLORS.default;
}

// =============================================================================
// Community Colors (Cluster assignment)
// =============================================================================

/**
 * Color palette for community clusters.
 * Communities are assigned colors in order from this palette.
 */
export const COMMUNITY_PALETTE: string[] = [
  '#f87171', // Red
  '#fb923c', // Orange
  '#fbbf24', // Amber
  '#a3e635', // Lime
  '#4ade80', // Green
  '#2dd4bf', // Teal
  '#22d3ee', // Cyan
  '#60a5fa', // Blue
  '#a78bfa', // Violet
  '#f472b6', // Pink
  '#94a3b8', // Slate (fallback)
];

/**
 * Assign a color to a community based on its index.
 */
export function getCommunityColor(index: number): string {
  return COMMUNITY_PALETTE[index % COMMUNITY_PALETTE.length];
}

// =============================================================================
// Confidence Colors (Edge opacity)
// =============================================================================

/**
 * Get opacity value based on confidence score.
 * Maps 0.0–1.0 confidence to 0.3–1.0 opacity so edges are never fully invisible.
 */
export function getConfidenceOpacity(confidence: number): number {
  const clamped = Math.max(0, Math.min(1, confidence));
  return 0.3 + clamped * 0.7;
}

/**
 * Convert a hex color to rgba with the given opacity.
 */
function hexToRgba(hex: string, opacity: number): string {
  const h = hex.replace('#', '');
  let r: number, g: number, b: number;

  if (h.length === 3) {
    r = parseInt(h[0] + h[0], 16);
    g = parseInt(h[1] + h[1], 16);
    b = parseInt(h[2] + h[2], 16);
  } else if (h.length === 6) {
    r = parseInt(h.substring(0, 2), 16);
    g = parseInt(h.substring(2, 4), 16);
    b = parseInt(h.substring(4, 6), 16);
  } else {
    return `rgba(156, 163, 175, ${opacity})`;
  }

  return `rgba(${r}, ${g}, ${b}, ${opacity})`;
}

/**
 * Get a hex color adjusted for a confidence score as an rgba string.
 */
export function getColorWithConfidence(baseColor: string, confidence: number): string {
  const opacity = getConfidenceOpacity(confidence);
  if (baseColor.startsWith('var(')) {
    const match = baseColor.match(/var\([^,]+,\s*([^)]+)\)/);
    if (match) return hexToRgba(match[1], opacity);
    return baseColor;
  }
  return hexToRgba(baseColor, opacity);
}

// =============================================================================
// Primary Entry Point
// =============================================================================

/**
 * Get the primary visualization color for an entity based on its parsed ID parts.
 * Colors by entity type (first ID segment): code/spec/task/loop/proposal/etc.
 *
 * Accepts either an EntityIdParts object or a plain string (type name or full ID).
 */
export function getEntityColor(idPartsOrType: EntityIdParts | string): string {
  if (typeof idPartsOrType === 'string') {
    // Accept both "code" (type name) and "code.file.main-go" (full ID)
    const type = idPartsOrType.includes('.') ? idPartsOrType.split('.')[0] : idPartsOrType;
    return getEntityTypeColor(type);
  }
  return getEntityTypeColor(idPartsOrType.type);
}
