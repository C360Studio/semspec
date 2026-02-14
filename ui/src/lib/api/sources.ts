/**
 * API client for sources management.
 * Handles document upload, listing, and reindexing.
 */
import { graphqlRequest } from './graphql';
import type { RawEntity } from './transforms';
import type {
	Source,
	DocumentSource,
	DocumentChunk,
	SourceWithDetail,
	UploadResponse,
	ReindexResponse,
	DocCategory,
	SourceStatus
} from '$lib/types/source';

const BASE_URL = import.meta.env.VITE_API_URL || '';

interface SourcesListParams {
	category?: DocCategory;
	query?: string;
	limit?: number;
}

/**
 * Transform a raw entity from graph to DocumentSource format.
 */
function transformToSource(raw: RawEntity): DocumentSource | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Skip chunk entities (they have chunk_index)
	if (predicates['source.doc.chunk_index'] !== undefined) {
		return null;
	}

	// Only process source entities
	const sourceType = predicates['source.type'] as string;
	if (sourceType !== 'document') {
		return null;
	}

	return {
		id: raw.id,
		type: 'document',
		name: (predicates['source.name'] as string) || raw.id.split('.').pop() || raw.id,
		status: (predicates['source.status'] as SourceStatus) || 'pending',
		addedAt: (predicates['source.added_at'] as string) || new Date().toISOString(),
		addedBy: predicates['source.added_by'] as string | undefined,
		project: predicates['source.project'] as string | undefined,
		error: predicates['source.error'] as string | undefined,
		filename: (predicates['source.doc.file_path'] as string)?.split('/').pop() || '',
		mimeType: (predicates['source.doc.mime_type'] as string) || 'text/plain',
		category: (predicates['source.doc.category'] as DocCategory) || 'reference',
		summary: predicates['source.doc.summary'] as string | undefined,
		requirements: predicates['source.doc.requirements'] as string[] | undefined,
		chunkCount: predicates['source.doc.chunk_count'] as number | undefined,
		appliesTo: predicates['source.doc.applies_to'] as string[] | undefined,
		severity: predicates['source.doc.severity'] as 'error' | 'warning' | 'info' | undefined,
		filePath: predicates['source.doc.file_path'] as string | undefined
	};
}

/**
 * Transform a raw entity to DocumentChunk format.
 */
function transformToChunk(raw: RawEntity, parentId: string): DocumentChunk | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Only process chunk entities
	const chunkIndex = predicates['source.doc.chunk_index'] as number | undefined;
	if (chunkIndex === undefined) {
		return null;
	}

	return {
		id: raw.id,
		parentId,
		index: chunkIndex,
		section: predicates['source.doc.section'] as string | undefined,
		content: (predicates['source.doc.content'] as string) || ''
	};
}

export const sourcesApi = {
	/**
	 * List all document sources with optional filtering.
	 */
	list: async (params?: SourcesListParams): Promise<DocumentSource[]> => {
		const limit = params?.limit || 200;

		const result = await graphqlRequest<{ entitiesByPrefix: RawEntity[] }>(
			`
			query($prefix: String!, $limit: Int) {
				entitiesByPrefix(prefix: $prefix, limit: $limit) {
					id
					triples { subject predicate object }
				}
			}
		`,
			{ prefix: 'source.doc.', limit }
		);

		let sources = result.entitiesByPrefix
			.map(transformToSource)
			.filter((s): s is DocumentSource => s !== null);

		// Apply category filter
		if (params?.category) {
			sources = sources.filter((s) => s.category === params.category);
		}

		// Apply search filter
		if (params?.query) {
			const q = params.query.toLowerCase();
			sources = sources.filter(
				(s) =>
					s.name.toLowerCase().includes(q) ||
					s.filename.toLowerCase().includes(q) ||
					s.summary?.toLowerCase().includes(q) ||
					s.id.toLowerCase().includes(q)
			);
		}

		// Sort by addedAt descending (newest first)
		sources.sort((a, b) => new Date(b.addedAt).getTime() - new Date(a.addedAt).getTime());

		return sources;
	},

	/**
	 * Get a single source by ID with its chunks.
	 */
	get: async (id: string): Promise<SourceWithDetail> => {
		// Get the main entity
		const mainResult = await graphqlRequest<{ entity: RawEntity }>(
			`
			query($id: String!) {
				entity(id: $id) {
					id
					triples { subject predicate object }
				}
			}
		`,
			{ id }
		);

		if (!mainResult.entity) {
			throw new Error('Source not found');
		}

		const source = transformToSource(mainResult.entity);
		if (!source) {
			throw new Error('Entity is not a document source');
		}

		// Get chunks by prefix
		const chunkPrefix = `${id}.chunk.`;
		const chunksResult = await graphqlRequest<{ entitiesByPrefix: RawEntity[] }>(
			`
			query($prefix: String!, $limit: Int) {
				entitiesByPrefix(prefix: $prefix, limit: $limit) {
					id
					triples { subject predicate object }
				}
			}
		`,
			{ prefix: chunkPrefix, limit: 100 }
		);

		const chunks = chunksResult.entitiesByPrefix
			.map((raw) => transformToChunk(raw, id))
			.filter((c): c is DocumentChunk => c !== null)
			.sort((a, b) => a.index - b.index);

		return {
			...source,
			chunks
		};
	},

	/**
	 * Upload a document for ingestion.
	 */
	upload: async (file: File, options?: { project?: string; category?: DocCategory }): Promise<UploadResponse> => {
		const formData = new FormData();
		formData.append('file', file);
		if (options?.project) {
			formData.append('project', options.project);
		}
		if (options?.category) {
			formData.append('category', options.category);
		}

		const response = await fetch(`${BASE_URL}/api/sources/docs`, {
			method: 'POST',
			body: formData
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Upload failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Delete a document source.
	 */
	delete: async (id: string): Promise<void> => {
		const response = await fetch(`${BASE_URL}/api/sources/docs/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Delete failed: ${response.status}`);
		}
	},

	/**
	 * Request re-ingestion of a document.
	 */
	reindex: async (id: string): Promise<ReindexResponse> => {
		const response = await fetch(`${BASE_URL}/api/sources/docs/${encodeURIComponent(id)}/reindex`, {
			method: 'POST'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Reindex failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Get chunks for a source (standalone method for lazy loading).
	 */
	getChunks: async (sourceId: string): Promise<DocumentChunk[]> => {
		const chunkPrefix = `${sourceId}.chunk.`;
		const result = await graphqlRequest<{ entitiesByPrefix: RawEntity[] }>(
			`
			query($prefix: String!, $limit: Int) {
				entitiesByPrefix(prefix: $prefix, limit: $limit) {
					id
					triples { subject predicate object }
				}
			}
		`,
			{ prefix: chunkPrefix, limit: 100 }
		);

		return result.entitiesByPrefix
			.map((raw) => transformToChunk(raw, sourceId))
			.filter((c): c is DocumentChunk => c !== null)
			.sort((a, b) => a.index - b.index);
	}
};
