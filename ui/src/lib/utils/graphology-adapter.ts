/**
 * Graphology Adapter
 *
 * Syncs graphStore state (entities + relationships) into a graphology
 * MultiDirectedGraph instance that Sigma.js consumes for rendering.
 *
 * The graphStore remains the source of truth. This adapter is a rendering
 * bridge only — it reads from the store and writes to the graphology graph.
 *
 * Ported from semdragon and adapted for semspec entity types.
 */

import type AbstractGraph from 'graphology';
type Graph = AbstractGraph;
import type { GraphEntity, GraphRelationship } from '$lib/stores/graphStore.svelte';
import { getEntityColor, getPredicateColor } from '$lib/utils/entity-colors';

const DEFAULT_NODE_SIZE = 8;
const MIN_NODE_SIZE = 5;
const MAX_NODE_SIZE = 20;

/**
 * Calculate node size based on connection count.
 * More connected entities render larger to visually convey hub status.
 */
function getNodeSize(entity: GraphEntity): number {
	const connections = entity.outgoing.length + entity.incoming.length;
	const size = DEFAULT_NODE_SIZE + Math.sqrt(connections) * 2;
	return Math.min(Math.max(size, MIN_NODE_SIZE), MAX_NODE_SIZE);
}

/**
 * Full sync: clear the graphology graph and rebuild from store data.
 *
 * Snapshots node positions before clearing so the FA2 layout is preserved
 * across incremental data refreshes. Without this, every sync would restart
 * the layout from random positions.
 */
export function syncStoreToGraph(
	graph: Graph,
	entities: GraphEntity[],
	relationships: GraphRelationship[]
): void {
	// Preserve positions so FA2 layout survives re-sync
	const positions = new Map<string, { x: number; y: number }>();
	graph.forEachNode((id: string, attrs: Record<string, unknown>) => {
		positions.set(id, { x: attrs.x as number, y: attrs.y as number });
	});

	graph.clear();

	for (const entity of entities) {
		const existing = positions.get(entity.id);
		graph.addNode(entity.id, {
			label: entity.label || entity.id,
			size: getNodeSize(entity),
			color: getEntityColor(entity.entityType),
			// "type" is reserved by Sigma as the WebGL program selector.
			// Store the semspec entity type as a separate attribute.
			entityType: entity.entityType,
			x: existing?.x ?? Math.random() * 100,
			y: existing?.y ?? Math.random() * 100
		});
	}

	for (const rel of relationships) {
		if (graph.hasNode(rel.sourceId) && graph.hasNode(rel.targetId)) {
			if (!graph.hasEdge(rel.id)) {
				graph.addEdgeWithKey(rel.id, rel.sourceId, rel.targetId, {
					label: rel.predicate.split('.').pop() ?? rel.predicate,
					color: getPredicateColor(rel.predicate),
					size: Math.max(1, rel.confidence * 3),
					type: 'arrow'
				});
			}
		}
	}
}

/**
 * Incremental add: add new nodes/edges without clearing the existing graph.
 * Used for entity expansion — the user double-clicks a node and we load its neighbors.
 *
 * New nodes are positioned near their already-visible neighbors to minimize
 * layout disruption.
 */
export function addToGraph(
	graph: Graph,
	entities: GraphEntity[],
	relationships: GraphRelationship[]
): void {
	for (const entity of entities) {
		if (!graph.hasNode(entity.id)) {
			const { x, y } = getInitialPosition(graph, entity);
			graph.addNode(entity.id, {
				label: entity.label || entity.id,
				size: getNodeSize(entity),
				color: getEntityColor(entity.entityType),
				entityType: entity.entityType,
				x,
				y
			});
		}
	}

	for (const rel of relationships) {
		if (
			graph.hasNode(rel.sourceId) &&
			graph.hasNode(rel.targetId) &&
			!graph.hasEdge(rel.id)
		) {
			graph.addEdgeWithKey(rel.id, rel.sourceId, rel.targetId, {
				label: rel.predicate.split('.').pop() ?? rel.predicate,
				color: getPredicateColor(rel.predicate),
				size: Math.max(1, rel.confidence * 3),
				type: 'arrow'
			});
		}
	}
}

/**
 * Get an initial position for a new node by averaging its existing neighbors'
 * positions with a small random jitter to avoid exact overlap.
 * Falls back to a random position if no neighbors are visible yet.
 */
function getInitialPosition(graph: Graph, entity: GraphEntity): { x: number; y: number } {
	const neighborIds = [
		...entity.outgoing.map((r) => r.targetId),
		...entity.incoming.map((r) => r.sourceId)
	];

	let sumX = 0;
	let sumY = 0;
	let count = 0;

	for (const nId of neighborIds) {
		if (graph.hasNode(nId)) {
			const attrs = graph.getNodeAttributes(nId);
			sumX += attrs.x as number;
			sumY += attrs.y as number;
			count++;
		}
	}

	if (count > 0) {
		return {
			x: sumX / count + (Math.random() - 0.5) * 20,
			y: sumY / count + (Math.random() - 0.5) * 20
		};
	}

	return { x: Math.random() * 100, y: Math.random() * 100 };
}
