/**
 * Knowledge Graph Store — Semspec
 *
 * Manages state for the graph visualization on the entities page.
 * Handles semspec entities, their relationships, selection state, and type filters.
 *
 * Uses Svelte 5 runes ($state, $derived) for reactivity.
 * SvelteMap/SvelteSet provide deep reactivity on collection mutations
 * without needing to replace the entire Map/Set reference on each update.
 *
 * Consumers read state directly via getters — no .subscribe() needed.
 */

import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import {
	type GraphEntity,
	type GraphRelationship,
	type GraphFilters,
	DEFAULT_GRAPH_FILTERS,
	parseEntityId,
	createRelationshipId,
	type TripleProperty
} from '$lib/api/graph-types';

// Re-export types so existing consumers can import from this module
export type { GraphEntity, GraphRelationship };

// =============================================================================
// Graph Store Adapter Interface
// =============================================================================
// The graph store expects data in a normalized shape (properties, outgoing,
// incoming). The adapter bridges the raw GraphQL API (graphApi.ts) to this
// shape — see the graphApiAdapter in routes/entities/+page.svelte.

/**
 * Adapter interface the graphStore requires for data loading.
 * Implemented as an inline adapter in the graph route page that bridges
 * graphApi + graphTransform to this normalized shape.
 */
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

/**
 * Default entity ID prefix filter for initial load.
 * Empty string loads across all domains.
 */
const DEFAULT_PREFIX = '';

/**
 * Default maximum nodes to load in a single query.
 * Keep moderate to avoid timeouts when the graph has many entities.
 * Users discover more via search and expand.
 */
const DEFAULT_NODE_LIMIT = 200;

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
	let filters = $state<GraphFilters>({ ...DEFAULT_GRAPH_FILTERS });
	let loading = $state(false);
	let error = $state<string | null>(null);

	// Entity type visibility toggles — starts empty, auto-populated on data load
	let visibleTypes = new SvelteSet<string>();

	// ---------------------------------------------------------------------------
	// Derived state
	// ---------------------------------------------------------------------------

	/** Filtered entity list applying search, type toggles, and confidence gate. */
	const filteredEntities = $derived.by(() => {
		let result = Array.from(entities.values());

		// Text search against entity ID and instance name
		if (filters.search) {
			const q = filters.search.toLowerCase();
			result = result.filter(
				(e) =>
					e.id.toLowerCase().includes(q) ||
					e.idParts.instance.toLowerCase().includes(q) ||
					e.idParts.type.toLowerCase().includes(q)
			);
		}

		// Entity type filter (explicit types[] in filters take precedence over visibleTypes toggle)
		if (filters.types.length > 0) {
			result = result.filter((e) => filters.types.includes(e.idParts.type));
		} else if (visibleTypes.size > 0) {
			// Use the visibility toggle set when no explicit type filter is active
			result = result.filter((e) => visibleTypes.has(e.idParts.type));
		}

		// Time range filter
		if (filters.timeRange) {
			const [start, end] = filters.timeRange;
			result = result.filter((e) =>
				e.properties.some((p) => p.timestamp >= start && p.timestamp <= end)
			);
		}

		return result;
	});

	/** Filtered relationships — only includes edges between visible entities. */
	const filteredRelationships = $derived.by(() => {
		const visibleIds = new Set(filteredEntities.map((e) => e.id));

		let result = Array.from(relationships.values()).filter(
			(r) => visibleIds.has(r.sourceId) && visibleIds.has(r.targetId)
		);

		if (filters.minConfidence > 0) {
			result = result.filter((r) => r.confidence >= filters.minConfidence);
		}

		if (filters.timeRange) {
			const [start, end] = filters.timeRange;
			result = result.filter((r) => r.timestamp >= start && r.timestamp <= end);
		}

		return result;
	});

	/** Unique entity types present in the current data set (from first ID segment). */
	const presentEntityTypes = $derived.by(() => {
		const types = new Set<string>();
		for (const e of entities.values()) {
			types.add(e.idParts.type);
		}
		return Array.from(types).sort();
	});

	/** Currently selected entity (full object, not just ID). */
	const selectedEntity = $derived(
		selectedEntityId ? entities.get(selectedEntityId) ?? null : null
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
		get filters() {
			return filters;
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

		// Derived
		get filteredEntities() {
			return filteredEntities;
		},
		get filteredRelationships() {
			return filteredRelationships;
		},
		get presentEntityTypes() {
			return presentEntityTypes;
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
			// A real error implies loading has ended
			if (value !== null) loading = false;
		},

		// =========================================================================
		// Entity Management
		// =========================================================================

		/**
		 * Add or update a single entity and index all its relationships.
		 */
		upsertEntity(entity: GraphEntity) {
			entities.set(entity.id, entity);
			for (const rel of entity.outgoing) {
				relationships.set(rel.id, rel);
			}
			for (const rel of entity.incoming) {
				relationships.set(rel.id, rel);
			}
		},

		/**
		 * Add or update multiple entities at once.
		 */
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

		/**
		 * Remove an entity and all relationships connected to it.
		 */
		removeEntity(entityId: string) {
			entities.delete(entityId);

			for (const [relId, rel] of relationships) {
				if (rel.sourceId === entityId || rel.targetId === entityId) {
					relationships.delete(relId);
				}
			}

			if (selectedEntityId === entityId) selectedEntityId = null;
		},

		/**
		 * Clear all entities, relationships, and selection state.
		 */
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

		clearExpanded() {
			expandedEntityIds.clear();
		},

		// =========================================================================
		// Filters
		// =========================================================================

		/** Merge a partial filter update into the current filters. */
		setFilters(newFilters: Partial<GraphFilters>) {
			filters = { ...filters, ...newFilters };
		},

		resetFilters() {
			filters = { ...DEFAULT_GRAPH_FILTERS };
		},

		// =========================================================================
		// Entity Type Visibility Toggles
		// =========================================================================

		/**
		 * Toggle an entity type on/off in the graph visualization.
		 * This operates independently of the filters.types array.
		 */
		toggleEntityType(type: string) {
			if (visibleTypes.has(type)) {
				visibleTypes.delete(type);
			} else {
				visibleTypes.add(type);
			}
		},

		/** Show all present entity types. */
		showAllTypes() {
			for (const t of presentEntityTypes) visibleTypes.add(t);
		},

		/** Hide all entity types (blank canvas). */
		hideAllTypes() {
			visibleTypes.clear();
		},

		// =========================================================================
		// Data Loading Methods
		// =========================================================================

		/**
		 * Load the initial graph with optional prefix and limit.
		 *
		 * @param api    - The GraphStoreAdapter instance
		 * @param prefix - Entity ID prefix filter; defaults to '' (all entities)
		 * @param limit  - Max nodes to load; defaults to 200
		 */
		async loadInitialGraph(
			api: GraphStoreAdapter,
			prefix: string = DEFAULT_PREFIX,
			limit: number = DEFAULT_NODE_LIMIT
		): Promise<void> {
			loading = true;
			error = null;

			try {
				const result = await api.listEntities({ prefix, limit });
				const newEntities = result.entities.map(buildGraphEntity);
				this.clearEntities();
				this.upsertEntities(newEntities);
				// Auto-populate visibleTypes from loaded data so all types are shown
				for (const e of newEntities) {
					visibleTypes.add(e.idParts.type);
				}
			} catch (err) {
				error = err instanceof Error ? err.message : 'Failed to load graph entities';
			} finally {
				loading = false;
			}
		},

		/**
		 * Expand an entity by loading its neighbors from the API.
		 * Marks the entity as expanded so it isn't re-fetched on subsequent clicks.
		 *
		 * @param api      - The GraphStoreAdapter instance
		 * @param entityId - The entity ID to expand
		 */
		async expandEntity(api: GraphStoreAdapter, entityId: string): Promise<void> {
			if (expandedEntityIds.has(entityId)) return;

			loading = true;
			try {
				const result = await api.getEntityNeighbors(entityId);
				const newEntities = result.entities.map(buildGraphEntity);
				this.upsertEntities(newEntities);
				// Auto-add new entity types to visibleTypes
				for (const e of newEntities) visibleTypes.add(e.idParts.type);
				expandedEntityIds.add(entityId);
			} catch (err) {
				if (err instanceof DOMException && err.name === 'AbortError') return;
				error = err instanceof Error ? err.message : `Failed to expand entity ${entityId}`;
			} finally {
				loading = false;
			}
		},

		/**
		 * Search for entities matching a query string.
		 * Merges results into the existing graph (additive, does not clear).
		 *
		 * @param api   - The GraphStoreAdapter instance
		 * @param query - Free-text or NLQ search string
		 */
		async searchEntities(api: GraphStoreAdapter, query: string): Promise<void> {
			if (!query.trim()) return;

			loading = true;
			error = null;

			try {
				const result = await api.searchEntities({ query, limit: DEFAULT_NODE_LIMIT });
				const newEntities = result.entities.map(buildGraphEntity);
				this.upsertEntities(newEntities);
				// Also update the search filter so the text box reflects the query
				filters = { ...filters, search: query };
				// Show newly discovered entity types
				for (const e of newEntities) visibleTypes.add(e.idParts.type);
			} catch (err) {
				error = err instanceof Error ? err.message : 'Search failed';
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
			filters = { ...DEFAULT_GRAPH_FILTERS };
			loading = false;
			error = null;
		}
	};
}

// =============================================================================
// Entity Builder Helper
// =============================================================================

/**
 * Build a GraphEntity from raw API response data.
 * Normalises timestamps to Unix milliseconds and constructs relationship IDs.
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
	const idParts = parseEntityId(data.id);

	const properties: TripleProperty[] = (data.properties ?? []).map((p) => ({
		predicate: p.predicate,
		object: p.object,
		confidence: p.confidence,
		source: p.source ?? 'unknown',
		timestamp: typeof p.timestamp === 'string' ? new Date(p.timestamp).getTime() : p.timestamp
	}));

	const outgoing: GraphRelationship[] = (data.outgoing ?? []).map((r) => ({
		id: createRelationshipId(data.id, r.predicate, r.targetId),
		sourceId: data.id,
		targetId: r.targetId,
		predicate: r.predicate,
		confidence: r.confidence,
		timestamp: r.timestamp
			? typeof r.timestamp === 'string'
				? new Date(r.timestamp).getTime()
				: r.timestamp
			: Date.now()
	}));

	const incoming: GraphRelationship[] = (data.incoming ?? []).map((r) => ({
		id: createRelationshipId(r.sourceId, r.predicate, data.id),
		sourceId: r.sourceId,
		targetId: data.id,
		predicate: r.predicate,
		confidence: r.confidence,
		timestamp: r.timestamp
			? typeof r.timestamp === 'string'
				? new Date(r.timestamp).getTime()
				: r.timestamp
			: Date.now()
	}));

	return {
		id: data.id,
		idParts,
		properties,
		outgoing,
		incoming
	};
}

// =============================================================================
// Export Singleton
// =============================================================================

export const graphStore = createGraphStore();
