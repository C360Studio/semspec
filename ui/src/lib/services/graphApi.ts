/**
 * Graph Gateway API client
 *
 * GraphQL client for the graph-gateway endpoint at /graphql.
 * Proxied by Vite (dev) and Caddy (prod) — no auth headers required for
 * same-origin requests.
 */

import type {
  BackendEntity,
  ClassificationMeta,
  GlobalSearchResult,
  PathSearchResult,
} from '$lib/api/graph-types';

const GRAPHQL_ENDPOINT = '/graphql';

// =============================================================================
// Error Type
// =============================================================================

export class GraphApiError extends Error {
  constructor(
    message: string,
    public statusCode: number,
    public details?: unknown,
  ) {
    super(message);
    this.name = 'GraphApiError';
  }
}

// =============================================================================
// Internal Fetch Helpers
// =============================================================================

interface GraphQLRequest {
  query: string;
  variables: Record<string, unknown>;
}

interface GraphQLResponse<T> {
  data?: T;
  errors?: Array<{
    message: string;
    path?: string[];
  }>;
  extensions?: Record<string, unknown>;
}

interface QueryResultWithExtensions<T> {
  data: T;
  extensions?: Record<string, unknown>;
}

async function executeQuery<T>(
  query: string,
  variables: Record<string, unknown>,
  operationName: string,
  signal?: AbortSignal,
): Promise<T> {
  const request: GraphQLRequest = { query, variables };

  let response: Response;
  try {
    response = await fetch(GRAPHQL_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
      ...(signal !== undefined ? { signal } : {}),
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error;
    }
    throw new GraphApiError(
      `Network error during ${operationName}: ${error instanceof Error ? error.message : 'Unknown error'}`,
      0,
      { originalError: error },
    );
  }

  if (!response.ok) {
    throw new GraphApiError(
      `${operationName} failed: ${response.statusText}`,
      response.status,
    );
  }

  let data: GraphQLResponse<T>;
  try {
    data = await response.json();
  } catch (error) {
    throw new GraphApiError(
      `Invalid JSON response from ${operationName}`,
      500,
      { originalError: error },
    );
  }

  if (data.errors && data.errors.length > 0) {
    throw new GraphApiError(data.errors[0].message, 200, {
      errors: data.errors,
    });
  }

  if (!data.data) {
    throw new GraphApiError(`No data in ${operationName} response`, 500);
  }

  return data.data;
}

/**
 * Like executeQuery but also returns GraphQL extensions (used for NLQ
 * classification metadata from semstreams alpha.17+).
 */
async function executeQueryWithExtensions<T>(
  query: string,
  variables: Record<string, unknown>,
  operationName: string,
  signal?: AbortSignal,
): Promise<QueryResultWithExtensions<T>> {
  const request: GraphQLRequest = { query, variables };

  let response: Response;
  try {
    response = await fetch(GRAPHQL_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
      ...(signal !== undefined ? { signal } : {}),
    });
  } catch (error) {
    // AbortError must propagate as-is — do not wrap in GraphApiError
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error;
    }
    throw new GraphApiError(
      `Network error during ${operationName}: ${error instanceof Error ? error.message : 'Unknown error'}`,
      0,
      { originalError: error },
    );
  }

  if (!response.ok) {
    throw new GraphApiError(
      `${operationName} failed: ${response.statusText}`,
      response.status,
    );
  }

  let parsed: GraphQLResponse<T>;
  try {
    parsed = await response.json();
  } catch (error) {
    throw new GraphApiError(
      `Invalid JSON response from ${operationName}`,
      500,
      { originalError: error },
    );
  }

  if (parsed.errors && parsed.errors.length > 0) {
    throw new GraphApiError(parsed.errors[0].message, 200, {
      errors: parsed.errors,
    });
  }

  if (!parsed.data) {
    throw new GraphApiError(`No data in ${operationName} response`, 500);
  }

  return { data: parsed.data, extensions: parsed.extensions };
}

// =============================================================================
// Public API
// =============================================================================

export const graphApi = {
  /**
   * Load entities matching a prefix — used for initial graph population.
   *
   * Typical prefixes for semspec:
   *   "code."     — all code entities
   *   "spec."     — all spec/requirement entities
   *   "semspec."  — all plan entities
   *   ""          — all entities (empty prefix loads everything)
   */
  async getEntitiesByPrefix(
    prefix: string,
    limit: number = 50,
    signal?: AbortSignal,
  ): Promise<BackendEntity[]> {
    const query = `
      query GetEntitiesByPrefix($prefix: String!, $limit: Int!) {
        entitiesByPrefix(prefix: $prefix, limit: $limit) {
          id
          triples {
            subject
            predicate
            object
          }
        }
      }
    `;

    const data = await executeQuery<{ entitiesByPrefix: BackendEntity[] }>(
      query,
      { prefix, limit },
      'getEntitiesByPrefix',
      signal,
    );

    return data.entitiesByPrefix;
  },

  /**
   * Expand neighbors around an entity — used for graph exploration.
   * Returns entities and edges within maxDepth hops, capped at maxNodes.
   */
  async pathSearch(
    startEntity: string,
    maxDepth: number = 3,
    maxNodes: number = 100,
    signal?: AbortSignal,
  ): Promise<PathSearchResult> {
    const query = `
      query PathSearch($startEntity: String!, $maxDepth: Int!, $maxNodes: Int!) {
        pathSearch(startEntity: $startEntity, maxDepth: $maxDepth, maxNodes: $maxNodes) {
          entities {
            id
            triples {
              subject
              predicate
              object
            }
          }
          edges {
            subject
            predicate
            object
          }
        }
      }
    `;

    const data = await executeQuery<{ pathSearch: PathSearchResult }>(
      query,
      { startEntity, maxDepth, maxNodes },
      'pathSearch',
      signal,
    );

    return data.pathSearch;
  },

  /**
   * Fetch a single entity by its full ID.
   * Throws GraphApiError with statusCode 404 if not found.
   */
  async getEntity(id: string, signal?: AbortSignal): Promise<BackendEntity> {
    const query = `
      query GetEntity($id: String!) {
        entity(id: $id) {
          id
          triples {
            subject
            predicate
            object
          }
        }
      }
    `;

    const data = await executeQuery<{ entity: BackendEntity | null }>(
      query,
      { id },
      'getEntity',
      signal,
    );

    if (!data.entity) {
      throw new GraphApiError(`Entity ${id} not found`, 404);
    }

    return data.entity;
  },

  /**
   * Execute a natural language query against the graph.
   *
   * Returns matching entities, community summaries, explicit relationships,
   * and result metadata. NLQ classification is available in the returned
   * `classification` field when the backend runs semstreams alpha.17+.
   *
   * Pass an AbortSignal to cancel in-flight requests on user input changes.
   */
  async globalSearch(
    query: string,
    level?: number,
    maxCommunities?: number,
    signal?: AbortSignal,
  ): Promise<GlobalSearchResult> {
    const gqlQuery = `
      query GlobalSearch($query: String!, $level: Int, $maxCommunities: Int) {
        globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
          entities {
            id
            triples {
              subject
              predicate
              object
            }
          }
          community_summaries {
            communityId
            text
            keywords
          }
          relationships {
            from
            to
            predicate
          }
          count
          duration_ms
        }
      }
    `;

    const variables: Record<string, unknown> = { query };
    if (level !== undefined) variables.level = level;
    if (maxCommunities !== undefined) variables.maxCommunities = maxCommunities;

    interface GlobalSearchGqlResponse {
      globalSearch: {
        entities: BackendEntity[];
        community_summaries: Array<{
          communityId: string;
          text: string;
          keywords: string[];
        }>;
        relationships: Array<{
          from: string;
          to: string;
          predicate: string;
        }>;
        count: number;
        duration_ms: number;
      };
    }

    const { data, extensions } =
      await executeQueryWithExtensions<GlobalSearchGqlResponse>(
        gqlQuery,
        variables,
        'globalSearch',
        signal,
      );

    const gs = data.globalSearch;

    // Extract NLQ classification from GraphQL extensions (semstreams alpha.17+)
    let classification: ClassificationMeta | undefined;
    if (extensions?.classification) {
      const c = extensions.classification as Record<string, unknown>;
      classification = {
        tier: (c.tier as number) ?? 0,
        confidence: (c.confidence as number) ?? 0,
        intent: (c.intent as string) ?? '',
      };
    }

    return {
      entities: gs.entities ?? [],
      communitySummaries: gs.community_summaries ?? [],
      relationships: gs.relationships ?? [],
      count: gs.count ?? 0,
      durationMs: gs.duration_ms ?? 0,
      classification,
    };
  },
};
