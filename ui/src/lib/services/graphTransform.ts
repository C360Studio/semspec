/**
 * Graph data transformation
 *
 * Converts raw GraphQL responses from graph-gateway into frontend
 * GraphEntity structures with resolved properties and relationships.
 *
 * Two entry points:
 * - transformPathSearchResult  — for pathSearch / getEntitiesByPrefix results
 * - transformGlobalSearchResult — for NLQ globalSearch results
 */

import type {
  GlobalSearchResult,
  GraphEntity,
  GraphRelationship,
  PathSearchResult,
  TripleProperty,
} from '$lib/api/graph-types';
import {
  createRelationshipId,
  isEntityReference,
  parseEntityId,
} from '$lib/api/graph-types';

// =============================================================================
// Path Search Transform
// =============================================================================

/**
 * Transform a PathSearchResult into an array of GraphEntity.
 *
 * Triple processing:
 * - object is an entity reference → outgoing GraphRelationship
 * - otherwise → TripleProperty (literal value)
 *
 * Explicit edges from the `edges` field are processed after triples and
 * wired bidirectionally (outgoing on source, incoming on target).
 */
export function transformPathSearchResult(
  result: PathSearchResult,
): GraphEntity[] {
  const entityMap = new Map<string, GraphEntity>();

  // Initialise entities
  for (const backendEntity of result.entities || []) {
    entityMap.set(backendEntity.id, {
      id: backendEntity.id,
      idParts: parseEntityId(backendEntity.id),
      properties: [],
      outgoing: [],
      incoming: [],
    });
  }

  // Process triples to extract properties and outgoing relationships
  for (const backendEntity of result.entities || []) {
    const entity = entityMap.get(backendEntity.id);
    if (!entity) continue;

    for (const triple of backendEntity.triples || []) {
      if (isEntityReference(triple.object)) {
        const relationship: GraphRelationship = {
          id: createRelationshipId(triple.subject, triple.predicate, triple.object),
          sourceId: triple.subject,
          targetId: triple.object,
          predicate: triple.predicate,
          confidence: 1.0,
          timestamp: 0,
        };
        entity.outgoing.push(relationship);
      } else {
        const property: TripleProperty = {
          predicate: triple.predicate,
          object: triple.object,
          confidence: 1.0,
          source: 'unknown',
          timestamp: 0,
        };
        entity.properties.push(property);
      }
    }
  }

  // Wire explicit edges bidirectionally
  for (const edge of result.edges || []) {
    const relationship: GraphRelationship = {
      id: createRelationshipId(edge.subject, edge.predicate, edge.object),
      sourceId: edge.subject,
      targetId: edge.object,
      predicate: edge.predicate,
      confidence: 1.0,
      timestamp: 0,
    };

    const sourceEntity = entityMap.get(edge.subject);
    if (sourceEntity) {
      sourceEntity.outgoing.push(relationship);
    }

    const targetEntity = entityMap.get(edge.object);
    if (targetEntity) {
      targetEntity.incoming.push(relationship);
    }
  }

  return Array.from(entityMap.values());
}

// =============================================================================
// Global Search Transform
// =============================================================================

/**
 * Transform a GlobalSearchResult (NLQ response) into an array of GraphEntity.
 *
 * Triple processing mirrors transformPathSearchResult.
 * Explicit SearchRelationship entries are processed after triples.
 * Duplicate relationship IDs are deduplicated per entity to avoid double-wiring
 * when both triples and explicit relationships reference the same edge.
 */
export function transformGlobalSearchResult(
  result: GlobalSearchResult,
): GraphEntity[] {
  const entityMap = new Map<string, GraphEntity>();

  // Initialise entities
  for (const backendEntity of result.entities || []) {
    entityMap.set(backendEntity.id, {
      id: backendEntity.id,
      idParts: parseEntityId(backendEntity.id),
      properties: [],
      outgoing: [],
      incoming: [],
    });
  }

  // Per-entity seen-sets for deduplication
  const outgoingIds = new Map<string, Set<string>>();
  const incomingIds = new Map<string, Set<string>>();
  for (const id of entityMap.keys()) {
    outgoingIds.set(id, new Set<string>());
    incomingIds.set(id, new Set<string>());
  }

  // Process triples
  for (const backendEntity of result.entities || []) {
    const entity = entityMap.get(backendEntity.id);
    if (!entity) continue;

    for (const triple of backendEntity.triples || []) {
      if (isEntityReference(triple.object)) {
        const relId = createRelationshipId(triple.subject, triple.predicate, triple.object);
        const seen = outgoingIds.get(entity.id)!;
        if (!seen.has(relId)) {
          seen.add(relId);
          entity.outgoing.push({
            id: relId,
            sourceId: triple.subject,
            targetId: triple.object,
            predicate: triple.predicate,
            confidence: 1.0,
            timestamp: 0,
          });
        }
      } else {
        entity.properties.push({
          predicate: triple.predicate,
          object: triple.object,
          confidence: 1.0,
          source: 'unknown',
          timestamp: 0,
        });
      }
    }
  }

  // Process explicit relationships — wire bidirectionally with deduplication
  for (const rel of result.relationships || []) {
    const relId = createRelationshipId(rel.from, rel.predicate, rel.to);
    const relationship: GraphRelationship = {
      id: relId,
      sourceId: rel.from,
      targetId: rel.to,
      predicate: rel.predicate,
      confidence: 1.0,
      timestamp: 0,
    };

    const sourceEntity = entityMap.get(rel.from);
    if (sourceEntity) {
      const seen = outgoingIds.get(rel.from)!;
      if (!seen.has(relId)) {
        seen.add(relId);
        sourceEntity.outgoing.push(relationship);
      }
    }

    const targetEntity = entityMap.get(rel.to);
    if (targetEntity) {
      const seen = incomingIds.get(rel.to)!;
      if (!seen.has(relId)) {
        seen.add(relId);
        targetEntity.incoming.push(relationship);
      }
    }
  }

  return Array.from(entityMap.values());
}
