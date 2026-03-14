/**
 * Knowledge Graph Store
 *
 * Manages state for the graph visualization on the entities page.
 * Handles semspec entities (code, spec, task, loop, proposal, activity),
 * their relationships, selection state, and type filters.
 *
 * Uses Svelte 5 runes ($state, $derived) for reactivity.
 * SvelteMap/SvelteSet provide fine-grained reactivity on collection mutations
 * without needing to replace the entire Map/Set reference on each update.
 *
 * Consumers read state directly via getters — no .subscribe() needed.
 *
 * Ported from semdragon graphStore and adapted for semspec entity types.
 */

import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import type { EntityType } from '$lib/types';

// =============================================================================
// Types
// =============================================================================

export interface GraphProperty {
	predicate: string;
	object: unknown;
	confidence: number;
	source?: string;
	timestamp: number;
}

export interface GraphRelationship {
	id: string;
	sourceId: string;
	targetId: string;
	predicate: string;
	confidence: number;
	timestamp: number;
}

export interface GraphEntity {
	id: string;
	/** Derived entity type from ID prefix (code, spec, task, loop, proposal, activity) */
	entityType: EntityType | 'unknown';
	/** Short label for display (last segment of ID) */
	label: string;
	properties: GraphProperty[];
	outgoing: GraphRelationship[];
	incoming: GraphRelationship[];
}

// =============================================================================
// Graph Store Adapter Interface
// =============================================================================
// The graph store expects data in a normalized shape. The adapter bridges the
// semspec API (api.entities.*) to this shape — implemented inline in the entities page.

export interface GraphStoreAdapter {
	listEntities(opts: { prefix?: string; limit?: number }): Promise<{
		entities: Array<{
			id: string;
			properties?: Array<{
				predicate: string;
				object: unknown;
				confidence: number;
				source?: string;
				timestamp: string | number;
			}>;
			outgoing?: Array<{
				predicate: string;
				targetId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
			incoming?: Array<{
				predicate: string;
				sourceId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
		}>;
	}>;

	getEntityNeighbors(entityId: string): Promise<{
		entities: Array<{
			id: string;
			properties?: Array<{
				predicate: string;
				object: unknown;
				confidence: number;
				source?: string;
				timestamp: string | number;
			}>;
			outgoing?: Array<{
				predicate: string;
				targetId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
			incoming?: Array<{
				predicate: string;
				sourceId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
		}>;
	}>;

	searchEntities(opts: { query: string; limit?: number }): Promise<{
		entities: Array<{
			id: string;
			properties?: Array<{
				predicate: string;
				object: unknown;
				confidence: number;
				source?: string;
				timestamp: string | number;
			}>;
			outgoing?: Array<{
				predicate: string;
				targetId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
			incoming?: Array<{
				predicate: string;
				sourceId: string;
				confidence: number;
				timestamp?: string | number;
			}>;
		}>;
	}>;
}

// =============================================================================
// Constants
// =============================================================================

const ALL_ENTITY_TYPES: EntityType[] = ['code', 'spec', 'task', 'loop', 'proposal', 'activity'];

const DEFAULT_NODE_LIMIT = 200;

// =============================================================================
// Helpers
// =============================================================================

/**
 * Derive semspec entity type from entity ID prefix.
 * Semspec IDs use dotted-prefix conventions: code.*, spec.*, task.*, etc.
 */
function getEntityType(id: string): EntityType | 'unknown' {
	const prefix = id.split('.')[0]?.toLowerCase() ?? '';
	if ((ALL_ENTITY_TYPES as string[]).includes(prefix)) {
		return prefix as EntityType;
	}
	return 'unknown';
}

/**
 * Derive a short display label from an entity ID.
 * Uses the last non-empty segment of the dotted ID.
 */
function getEntityLabel(id: string): string {
	const parts = id.split('.');
	// Walk backward to find the first non-empty segment
	for (let i = parts.length - 1; i >= 0; i--) {
		if (parts[i]) return parts[i];
	}
	return id;
}

/**
 * Create a stable relationship ID from source, predicate, and target.
 */
function createRelationshipId(sourceId: string, predicate: string, targetId: string): string {
	return `${sourceId}__${predicate}__${targetId}`;
}

/**
 * Normalize a timestamp to Unix milliseconds.
 */
function normalizeTimestamp(ts: string | number | undefined): number {
	if (ts === undefined) return Date.now();
	if (typeof ts === 'number') return ts;
	const parsed = new Date(ts).getTime();
	return isNaN(parsed) ? Date.now() : parsed;
}

/**
 * Build a GraphEntity from raw API response data.
 */
export function buildGraphEntity(data: {
	id: string;
	properties?: Array<{
		predicate: string;
		object: unknown;
		confidence: number;
		source?: string;
		timestamp: string | number;
	}>;
	outgoing?: Array<{
		predicate: string;
		targetId: string;
		confidence: number;
		timestamp?: string | number;
	}>;
	incoming?: Array<{
		predicate: string;
		sourceId: string;
		confidence: number;
		timestamp?: string | number;
	}>;
}): GraphEntity {
	const properties: GraphProperty[] = (data.properties ?? []).map((p) => ({
		predicate: p.predicate,
		object: p.object,
		confidence: p.confidence,
		source: p.source ?? 'unknown',
		timestamp: normalizeTimestamp(p.timestamp)
	}));

	const outgoing: GraphRelationship[] = (data.outgoing ?? []).map((r) => ({
		id: createRelationshipId(data.id, r.predicate, r.targetId),
		sourceId: data.id,
		targetId: r.targetId,
		predicate: r.predicate,
		confidence: r.confidence,
		timestamp: normalizeTimestamp(r.timestamp)
	}));

	const incoming: GraphRelationship[] = (data.incoming ?? []).map((r) => ({
		id: createRelationshipId(r.sourceId, r.predicate, data.id),
		sourceId: r.sourceId,
		targetId: data.id,
		predicate: r.predicate,
		confidence: r.confidence,
		timestamp: normalizeTimestamp(r.timestamp)
	}));

	return {
		id: data.id,
		entityType: getEntityType(data.id),
		label: getEntityLabel(data.id),
		properties,
		outgoing,
		incoming
	};
}

// =============================================================================
// Store Factory
// =============================================================================

function createGraphStore() {
	// ---------------------------------------------------------------------------
	// Reactive state
	// SvelteMap/SvelteSet trigger fine-grained reactivity on .set()/.add()/.delete()
	// without needing to replace the entire collection reference.
	// ---------------------------------------------------------------------------
	let entities = new SvelteMap<string, GraphEntity>();
	let relationships = new SvelteMap<string, GraphRelationship>();
	let selectedEntityId = $state<string | null>(null);
	let hoveredEntityId = $state<string | null>(null);
	let expandedEntityIds = new SvelteSet<string>();
	let loading = $state(false);
	let error = $state<string | null>(null);
	let searchQuery = $state('');

	// Entity type visibility toggles — all types visible by default
	let visibleTypes = new SvelteSet<EntityType>(ALL_ENTITY_TYPES);

	// ---------------------------------------------------------------------------
	// Derived state
	// ---------------------------------------------------------------------------

	/** Filtered entity list applying search query and type toggles. */
	const filteredEntities = $derived.by(() => {
		let result = Array.from(entities.values());

		// Text search against entity ID and label
		if (searchQuery) {
			const q = searchQuery.toLowerCase();
			result = result.filter(
				(e) => e.id.toLowerCase().includes(q) || e.label.toLowerCase().includes(q)
			);
		}

		// Entity type visibility filter
		result = result.filter((e) => visibleTypes.has(e.entityType as EntityType));

		return result;
	});

	/** Filtered relationships — only includes edges between currently visible entities. */
	const filteredRelationships = $derived.by(() => {
		const visibleIds = new Set(filteredEntities.map((e) => e.id));
		return Array.from(relationships.values()).filter(
			(r) => visibleIds.has(r.sourceId) && visibleIds.has(r.targetId)
		);
	});

	/** Currently selected entity (full object, not just ID). */
	const selectedEntity = $derived(
		selectedEntityId ? (entities.get(selectedEntityId) ?? null) : null
	);

	return {
		// -------------------------------------------------------------------------
		// State getters — read directly, no .subscribe() needed
		// -------------------------------------------------------------------------

		get entities() {
			return entities;
		},
		get relationships() {
			return relationships;
		},
		get selectedEntityId() {
			return selectedEntityId;
		},
		get hoveredEntityId() {
			return hoveredEntityId;
		},
		get expandedEntityIds() {
			return expandedEntityIds;
		},
		get loading() {
			return loading;
		},
		get error() {
			return error;
		},
		get visibleTypes() {
			return visibleTypes;
		},
		get searchQuery() {
			return searchQuery;
		},

		// Derived
		get filteredEntities() {
			return filteredEntities;
		},
		get filteredRelationships() {
			return filteredRelationships;
		},
		get selectedEntity() {
			return selectedEntity;
		},

		// =========================================================================
		// Loading State
		// =========================================================================

		setLoading(value: boolean) {
			loading = value;
		},

		setError(value: string | null) {
			error = value;
			if (value !== null) loading = false;
		},

		// =========================================================================
		// Entity Management
		// =========================================================================

		/** Add or update a single entity and index all its relationships. */
		upsertEntity(entity: GraphEntity) {
			entities.set(entity.id, entity);
			for (const rel of entity.outgoing) {
				relationships.set(rel.id, rel);
			}
			for (const rel of entity.incoming) {
				relationships.set(rel.id, rel);
			}
		},

		/** Add or update multiple entities at once. */
		upsertEntities(newEntities: GraphEntity[]) {
			for (const entity of newEntities) {
				entities.set(entity.id, entity);
				for (const rel of entity.outgoing) {
					relationships.set(rel.id, rel);
				}
				for (const rel of entity.incoming) {
					relationships.set(rel.id, rel);
				}
			}
		},

		/** Clear all entities, relationships, and selection state. */
		clearEntities() {
			entities.clear();
			relationships.clear();
			selectedEntityId = null;
			hoveredEntityId = null;
			expandedEntityIds.clear();
		},

		// =========================================================================
		// Selection State
		// =========================================================================

		selectEntity(entityId: string | null) {
			selectedEntityId = entityId;
		},

		setHoveredEntity(entityId: string | null) {
			hoveredEntityId = entityId;
		},

		markExpanded(entityId: string) {
			expandedEntityIds.add(entityId);
		},

		isExpanded(entityId: string): boolean {
			return expandedEntityIds.has(entityId);
		},

		// =========================================================================
		// Filters
		// =========================================================================

		setSearch(query: string) {
			searchQuery = query;
		},

		/** Toggle a semspec entity type on/off in the graph visualization. */
		toggleType(type: EntityType) {
			if (visibleTypes.has(type)) {
				visibleTypes.delete(type);
			} else {
				visibleTypes.add(type);
			}
		},

		/** Show all entity types. */
		showAllTypes() {
			for (const t of ALL_ENTITY_TYPES) visibleTypes.add(t);
		},

		/** Hide all entity types (blank canvas). */
		hideAllTypes() {
			visibleTypes.clear();
		},

		// =========================================================================
		// Data Loading
		// =========================================================================

		/**
		 * Load the initial graph with optional limit.
		 */
		async loadInitialGraph(adapter: GraphStoreAdapter, limit: number = DEFAULT_NODE_LIMIT): Promise<void> {
			loading = true;
			error = null;

			try {
				const result = await adapter.listEntities({ limit });
				const newEntities = result.entities.map(buildGraphEntity);
				this.clearEntities();
				this.upsertEntities(newEntities);
			} catch (err) {
				error = err instanceof Error ? err.message : 'Failed to load graph entities';
			} finally {
				loading = false;
			}
		},

		/**
		 * Expand an entity by loading its neighbors from the API.
		 * Marks the entity as expanded so it isn't re-fetched on subsequent clicks.
		 */
		async expandEntity(adapter: GraphStoreAdapter, entityId: string): Promise<void> {
			if (expandedEntityIds.has(entityId)) return;

			loading = true;
			try {
				const result = await adapter.getEntityNeighbors(entityId);
				const newEntities = result.entities.map(buildGraphEntity);
				this.upsertEntities(newEntities);
				expandedEntityIds.add(entityId);
			} catch (err) {
				if (err instanceof DOMException && err.name === 'AbortError') return;
				error = err instanceof Error ? err.message : `Failed to expand entity ${entityId}`;
			} finally {
				loading = false;
			}
		},

		// =========================================================================
		// Reset
		// =========================================================================

		reset() {
			entities.clear();
			relationships.clear();
			selectedEntityId = null;
			hoveredEntityId = null;
			expandedEntityIds.clear();
			searchQuery = '';
			loading = false;
			error = null;
			for (const t of ALL_ENTITY_TYPES) visibleTypes.add(t);
		}
	};
}

// =============================================================================
// Export Singleton
// =============================================================================

export const graphStore = createGraphStore();
