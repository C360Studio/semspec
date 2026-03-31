/**
 * Knowledge Graph Types — Semspec
 *
 * Types for visualizing the semantic knowledge graph via the graph-gateway
 * GraphQL endpoint at /graphql.
 *
 * The graph uses RDF-like triples (Subject-Predicate-Object) where:
 * - Entities have variable-length dotted IDs: e.g. "code.file.main-go",
 *   "spec.requirement.abc", "semspec.plan.my-plan"
 * - Relationships are triples where the object references another entity
 * - Properties are triples where the object is a literal value
 *
 * Semspec entity types are derived from the first dot-segment of the ID:
 * code, spec, task, loop, proposal, activity, source, agent, semspec, etc.
 */

// =============================================================================
// Entity ID Types
// =============================================================================

/**
 * Parsed components of a semspec entity ID.
 *
 * Entity IDs follow the 6-part convention: org.platform.domain.system.type.instance
 * For 6+ part IDs, type is extracted from position 5 (index 4).
 * For shorter IDs (predicates, short refs), type is the first segment.
 */
export interface EntityIdParts {
  /** Semantic type — position 5 for 6-part IDs, first segment otherwise. */
  type: string;
  /** Last segment of the dotted ID — the leaf identifier. */
  instance: string;
  /** Segments before the type, dot-joined. */
  prefix: string;
  /** The original full entity ID. */
  raw: string;
}

// =============================================================================
// Core Graph Types
// =============================================================================

/**
 * A triple property representing a fact about an entity.
 * When object is a literal value, it is a property.
 * When object is another entity ID, it becomes a relationship.
 */
export interface TripleProperty {
  /** 3-part dotted notation: domain.category.property (e.g. "dc.terms.title") */
  predicate: string;
  /** Literal value (number, string, boolean) or entity ID reference */
  object: unknown;
  /** 0.0 - 1.0 */
  confidence: number;
  /** Origin component that created this fact */
  source: string;
  /** Unix milliseconds */
  timestamp: number;
}

/**
 * A relationship between two entities (edge in the graph).
 * Created from triples where the object references another entity.
 */
export interface GraphRelationship {
  /** Unique relationship ID — "sourceId:predicate:targetId" */
  id: string;
  sourceId: string;
  targetId: string;
  /** Relationship type (e.g. "spec.rel.implements") */
  predicate: string;
  /** 0.0 - 1.0 */
  confidence: number;
  /** Unix milliseconds */
  timestamp: number;
}

/**
 * A graph entity (node in the graph).
 */
export interface GraphEntity {
  /** Full entity ID */
  id: string;
  /** Parsed ID components */
  idParts: EntityIdParts;
  /** Literal-valued triples */
  properties: TripleProperty[];
  /** Relationships where this entity is source */
  outgoing: GraphRelationship[];
  /** Relationships where this entity is target */
  incoming: GraphRelationship[];
}

// =============================================================================
// API Response Types (raw from GraphQL)
// =============================================================================

/**
 * Backend triple structure from GraphQL API.
 */
export interface BackendTriple {
  subject: string;
  predicate: string;
  object: unknown;
}

/**
 * Backend entity structure from GraphQL API.
 */
export interface BackendEntity {
  id: string;
  triples: BackendTriple[];
}

/**
 * Backend edge structure from GraphQL API.
 */
export interface BackendEdge {
  subject: string;
  predicate: string;
  object: string;
}

/**
 * GraphQL pathSearch query result.
 */
export interface PathSearchResult {
  entities: BackendEntity[];
  edges: BackendEdge[];
}

/**
 * Community summary returned by a global (NLQ) search.
 */
export interface CommunitySummary {
  communityId: string;
  text: string;
  keywords: string[];
}

/**
 * Explicit relationship returned by a global (NLQ) search.
 */
export interface SearchRelationship {
  from: string;
  to: string;
  predicate: string;
}

/**
 * NLQ classification metadata returned via GraphQL extensions.
 * Available in semstreams alpha.17+.
 */
export interface ClassificationMeta {
  tier: number;
  confidence: number;
  intent: string;
}

/**
 * Parsed result from the globalSearch GraphQL operation.
 */
export interface GlobalSearchResult {
  entities: BackendEntity[];
  communitySummaries: CommunitySummary[];
  relationships: SearchRelationship[];
  count: number;
  durationMs: number;
  classification?: ClassificationMeta;
}

// =============================================================================
// Semspec Entity Types
// =============================================================================

/**
 * Semspec entity types — derived from position 5 of 6-part entity IDs.
 */
export type SemspecEntityType =
  // Semsource code entities
  | 'file' | 'folder' | 'function' | 'class' | 'module' | 'package'
  | 'interface' | 'method' | 'field' | 'const' | 'config'
  // Semspec workflow entities
  | 'plan' | 'requirement' | 'scenario'
  // Agent/execution entities
  | 'task' | 'loop' | 'proposal' | 'activity' | 'agent'
  | 'unknown';

/**
 * Known entity ID first-segment prefixes for detecting entity references
 * in triple objects. A string is treated as an entity reference when its
 * first dot-segment matches one of these.
 */
const KNOWN_ENTITY_PREFIXES = new Set([
  'code',
  'spec',
  'task',
  'loop',
  'proposal',
  'activity',
  'source',
  'agent',
  'semspec',
]);

// =============================================================================
// Utility Functions
// =============================================================================

/**
 * Parse a semspec entity ID into its components.
 *
 * 6-part IDs (org.platform.domain.system.type.instance):
 *   "semspec.semsource.golang.workspace.file.main-go"  → type=file
 *   "semspec.workspace.wf.plan.plan.abc123"             → type=plan
 *
 * Shorter IDs (predicates, short refs):
 *   "code.artifact.path"  → type=code
 *   "source"              → type=source
 *
 * Returns sensible defaults rather than throwing for short or malformed IDs.
 */
export function parseEntityId(id: string): EntityIdParts {
  const parts = id.split('.');

  if (parts.length === 0 || !id) {
    return { type: 'unknown', instance: id, prefix: '', raw: id };
  }

  if (parts.length === 1) {
    return { type: parts[0], instance: parts[0], prefix: '', raw: id };
  }

  // 6-part IDs: org.platform.domain.system.type.instance
  // Type is at position 5 (index 4) — the semantic type segment.
  if (parts.length >= 6) {
    const type = parts[4];
    const instance = parts[parts.length - 1];
    const prefix = parts.slice(0, 4).join('.');
    return { type, instance, prefix, raw: id };
  }

  // Shorter IDs: first segment is type (predicates, short refs)
  const type = parts[0];
  const instance = parts[parts.length - 1];
  const prefix = parts.slice(1, -1).join('.');
  return { type, instance, prefix, raw: id };
}

/**
 * Generate a unique relationship ID from its three components.
 */
export function createRelationshipId(
  sourceId: string,
  predicate: string,
  targetId: string,
): string {
  return `${sourceId}:${predicate}:${targetId}`;
}

/**
 * Check if a triple's object is an entity reference (vs literal value).
 * An entity reference is a string containing dots where the first segment
 * matches a known semspec entity type prefix.
 */
export function isEntityReference(object: unknown): object is string {
  if (typeof object !== 'string') return false;
  if (!object.includes('.')) return false;
  const firstSegment = object.split('.')[0];
  return KNOWN_ENTITY_PREFIXES.has(firstSegment);
}

/**
 * Get display label for an entity — prefers human-readable names from triples,
 * falls back to the instance part of the ID.
 *
 * Semspec predicate priority:
 *   1. dc.terms.title  — most entities that have a human title
 *   2. code.artifact.path — code entities (extract filename from path)
 *   3. source.doc.file_path — source document entities
 *   4. source.identity.name — source identity
 *   5. spec.requirement.title — spec entities
 *   6. semspec.plan.title — plan entities
 *   7. instance segment of entity ID
 */
export function getEntityLabel(entity: GraphEntity): string {
  const fallback = entity.idParts.instance || entity.id;

  const val = (pred: string): string => {
    const t = entity.properties.find((tr) => tr.predicate === pred);
    if (!t || t.object == null) return '';
    return String(t.object);
  };

  // dc.terms.title is the universal human-readable label
  const dcTitle = val('dc.terms.title');
  if (dcTitle) return dcTitle;

  const type = entity.idParts.type;

  switch (type) {
    // Semsource code entities
    case 'file':
    case 'folder':
    case 'function':
    case 'class':
    case 'module':
    case 'package':
    case 'interface':
    case 'method':
    case 'field':
    case 'const':
    case 'config':
    case 'code': {
      const path = val('code.artifact.path');
      if (path) {
        const segments = path.split('/');
        return segments[segments.length - 1] || path;
      }
      return val('source.identity.name') || fallback;
    }
    // Semspec workflow entities
    case 'plan':
      return val('semspec.plan.title') || val('workflow.plan.title') || fallback;
    case 'requirement':
      return val('spec.requirement.title') || val('workflow.requirement.title') || fallback;
    case 'scenario':
      return val('workflow.scenario.title') || fallback;
    // Source documents
    case 'source':
      return (
        val('source.doc.file_path') ||
        val('source.identity.name') ||
        val('source.doc.summary') ||
        fallback
      );
    // Legacy first-segment types
    case 'spec':
      return val('spec.requirement.title') || fallback;
    case 'semspec':
      return val('semspec.plan.title') || fallback;
    default:
      return fallback;
  }
}

/**
 * Get the entity type label from its parsed ID parts.
 */
export function getEntityTypeLabel(entity: GraphEntity): string {
  return entity.idParts.type || 'unknown';
}

// =============================================================================
// Filter Types
// =============================================================================

/**
 * Filters for the knowledge graph visualization.
 */
export interface GraphFilters {
  search: string;
  /** Entity types to show (from first segment of entity ID). Empty = show all. */
  types: string[];
  /** Hide edges below this confidence score (0.0 - 1.0). */
  minConfidence: number;
  /** Unix ms range, null = all time. */
  timeRange: [number, number] | null;
}

/**
 * Default filter values — show all entities with no restrictions.
 */
export const DEFAULT_GRAPH_FILTERS: GraphFilters = {
  search: '',
  types: [],
  minConfidence: 0,
  timeRange: null,
};
