/**
 * API client for sources management.
 * Handles document upload, listing, and reindexing.
 */
import { graphqlRequest } from './graphql';
import type { RawEntity } from './transforms';
import type {
	Source,
	DocumentSource,
	RepositorySource,
	WebSource,
	DocumentChunk,
	WebChunk,
	SourceWithDetail,
	RepositoryWithDetail,
	WebSourceWithDetail,
	UploadResponse,
	ReindexResponse,
	WebSourceResponse,
	RefreshResponse,
	DocCategory,
	SourceStatus,
	AddRepositoryRequest,
	UpdateRepositoryRequest,
	AddWebSourceRequest,
	UpdateWebSourceRequest
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
function transformToDocumentSource(raw: RawEntity): DocumentSource | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Skip chunk entities (they have chunk_index)
	if (predicates['source.doc.chunk_index'] !== undefined) {
		return null;
	}

	// Only process document source entities
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
 * Transform a raw entity from graph to RepositorySource format.
 */
function transformToRepositorySource(raw: RawEntity): RepositorySource | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Only process repository source entities
	const sourceType = predicates['source.type'] as string;
	if (sourceType !== 'repository') {
		return null;
	}

	return {
		id: raw.id,
		type: 'repository',
		name: (predicates['source.name'] as string) || raw.id.split('.').pop() || raw.id,
		status: (predicates['source.status'] as SourceStatus) || 'pending',
		addedAt: (predicates['source.added_at'] as string) || new Date().toISOString(),
		addedBy: predicates['source.added_by'] as string | undefined,
		project: predicates['source.project'] as string | undefined,
		error: predicates['source.error'] as string | undefined,
		url: (predicates['source.repo.url'] as string) || '',
		branch: (predicates['source.repo.branch'] as string) || 'main',
		languages: predicates['source.repo.languages'] as string[] | undefined,
		entityCount: predicates['source.repo.entity_count'] as number | undefined,
		lastIndexed: predicates['source.repo.last_indexed'] as string | undefined,
		autoPull: predicates['source.repo.auto_pull'] as boolean | undefined,
		pullInterval: predicates['source.repo.pull_interval'] as string | undefined,
		lastCommit: predicates['source.repo.last_commit'] as string | undefined
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

/**
 * Transform a raw entity from graph to WebSource format.
 */
function transformToWebSource(raw: RawEntity): WebSource | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Skip chunk entities (they have chunk_index)
	if (predicates['source.web.chunk_index'] !== undefined) {
		return null;
	}

	// Only process web source entities
	const sourceType = predicates['source.type'] as string;
	if (sourceType !== 'web') {
		return null;
	}

	return {
		id: raw.id,
		type: 'web',
		name: (predicates['source.name'] as string) || (predicates['source.web.title'] as string) || raw.id.split('.').pop() || raw.id,
		status: (predicates['source.status'] as SourceStatus) || 'pending',
		addedAt: (predicates['source.added_at'] as string) || new Date().toISOString(),
		addedBy: predicates['source.added_by'] as string | undefined,
		project: predicates['source.project'] as string | undefined,
		error: predicates['source.error'] as string | undefined,
		url: (predicates['source.web.url'] as string) || '',
		contentType: predicates['source.web.content_type'] as string | undefined,
		title: predicates['source.web.title'] as string | undefined,
		lastFetched: predicates['source.web.last_fetched'] as string | undefined,
		etag: predicates['source.web.etag'] as string | undefined,
		contentHash: predicates['source.web.content_hash'] as string | undefined,
		autoRefresh: predicates['source.web.auto_refresh'] as boolean | undefined,
		refreshInterval: predicates['source.web.refresh_interval'] as string | undefined,
		chunkCount: predicates['source.web.chunk_count'] as number | undefined
	};
}

/**
 * Transform a raw entity to WebChunk format.
 */
function transformToWebChunk(raw: RawEntity, parentId: string): WebChunk | null {
	const predicates: Record<string, unknown> = {};
	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;
	}

	// Only process chunk entities
	const chunkIndex = predicates['source.web.chunk_index'] as number | undefined;
	if (chunkIndex === undefined) {
		return null;
	}

	return {
		id: raw.id,
		parentId,
		index: chunkIndex,
		section: predicates['source.web.section'] as string | undefined,
		content: (predicates['source.web.content'] as string) || ''
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
			.map(transformToDocumentSource)
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
	 * Get a single document source by ID with its chunks.
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

		const source = transformToDocumentSource(mainResult.entity);
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
	},

	// Repository API methods

	/**
	 * List all repository sources.
	 */
	listRepos: async (params?: { query?: string; limit?: number }): Promise<RepositorySource[]> => {
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
			{ prefix: 'source.repo.', limit }
		);

		let repos = result.entitiesByPrefix
			.map(transformToRepositorySource)
			.filter((s): s is RepositorySource => s !== null);

		// Apply search filter
		if (params?.query) {
			const q = params.query.toLowerCase();
			repos = repos.filter(
				(r) =>
					r.name.toLowerCase().includes(q) ||
					r.url.toLowerCase().includes(q) ||
					r.id.toLowerCase().includes(q)
			);
		}

		// Sort by addedAt descending (newest first)
		repos.sort((a, b) => new Date(b.addedAt).getTime() - new Date(a.addedAt).getTime());

		return repos;
	},

	/**
	 * Get a single repository by ID.
	 */
	getRepo: async (id: string): Promise<RepositoryWithDetail> => {
		const result = await graphqlRequest<{ entity: RawEntity }>(
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

		if (!result.entity) {
			throw new Error('Repository not found');
		}

		const repo = transformToRepositorySource(result.entity);
		if (!repo) {
			throw new Error('Entity is not a repository source');
		}

		return repo;
	},

	/**
	 * Add a new repository.
	 */
	addRepo: async (request: AddRepositoryRequest): Promise<{ id: string; status: string; message?: string }> => {
		const response = await fetch(`${BASE_URL}/api/sources/repos`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				url: request.url,
				branch: request.branch,
				project: request.project,
				auto_pull: request.autoPull,
				pull_interval: request.pullInterval
			})
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Add repository failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Delete a repository.
	 */
	deleteRepo: async (id: string): Promise<void> => {
		const response = await fetch(`${BASE_URL}/api/sources/repos/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Delete repository failed: ${response.status}`);
		}
	},

	/**
	 * Trigger a pull for a repository.
	 */
	pullRepo: async (id: string): Promise<{ id: string; status: string; lastCommit?: string; message?: string }> => {
		const response = await fetch(`${BASE_URL}/api/sources/repos/${encodeURIComponent(id)}/pull`, {
			method: 'POST'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Pull failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Trigger re-indexing of a repository.
	 */
	reindexRepo: async (id: string): Promise<{ id: string; status: string; message?: string }> => {
		const response = await fetch(`${BASE_URL}/api/sources/repos/${encodeURIComponent(id)}/reindex`, {
			method: 'POST'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Reindex failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Update repository settings.
	 */
	updateRepo: async (id: string, settings: UpdateRepositoryRequest): Promise<{ id: string; status: string; message?: string }> => {
		const response = await fetch(`${BASE_URL}/api/sources/repos/${encodeURIComponent(id)}`, {
			method: 'PATCH',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				auto_pull: settings.autoPull,
				pull_interval: settings.pullInterval,
				project: settings.project
			})
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Update failed: ${response.status}`);
		}

		return response.json();
	},

	// Web Source API methods

	/**
	 * List all web sources.
	 */
	listWeb: async (params?: { query?: string; limit?: number }): Promise<WebSource[]> => {
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
			{ prefix: 'source.web.', limit }
		);

		let sources = result.entitiesByPrefix
			.map(transformToWebSource)
			.filter((s): s is WebSource => s !== null);

		// Apply search filter
		if (params?.query) {
			const q = params.query.toLowerCase();
			sources = sources.filter(
				(s) =>
					s.name.toLowerCase().includes(q) ||
					s.url.toLowerCase().includes(q) ||
					s.title?.toLowerCase().includes(q) ||
					s.id.toLowerCase().includes(q)
			);
		}

		// Sort by addedAt descending (newest first)
		sources.sort((a, b) => new Date(b.addedAt).getTime() - new Date(a.addedAt).getTime());

		return sources;
	},

	/**
	 * Get a single web source by ID with its chunks.
	 */
	getWeb: async (id: string): Promise<WebSourceWithDetail> => {
		const result = await graphqlRequest<{ entity: RawEntity }>(
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

		if (!result.entity) {
			throw new Error('Web source not found');
		}

		const source = transformToWebSource(result.entity);
		if (!source) {
			throw new Error('Entity is not a web source');
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
			.map((raw) => transformToWebChunk(raw, id))
			.filter((c): c is WebChunk => c !== null)
			.sort((a, b) => a.index - b.index);

		return {
			...source,
			chunks
		};
	},

	/**
	 * Add a new web source.
	 */
	addWeb: async (request: AddWebSourceRequest): Promise<WebSourceResponse> => {
		const response = await fetch(`${BASE_URL}/api/sources/web`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				url: request.url,
				project: request.project,
				auto_refresh: request.autoRefresh,
				refresh_interval: request.refreshInterval
			})
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Add web source failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Delete a web source.
	 */
	deleteWeb: async (id: string): Promise<void> => {
		const response = await fetch(`${BASE_URL}/api/sources/web/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Delete web source failed: ${response.status}`);
		}
	},

	/**
	 * Trigger a refresh for a web source.
	 */
	refreshWeb: async (id: string): Promise<RefreshResponse> => {
		const response = await fetch(`${BASE_URL}/api/sources/web/${encodeURIComponent(id)}/refresh`, {
			method: 'POST'
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Refresh failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Update web source settings.
	 */
	updateWeb: async (id: string, settings: UpdateWebSourceRequest): Promise<WebSourceResponse> => {
		const response = await fetch(`${BASE_URL}/api/sources/web/${encodeURIComponent(id)}`, {
			method: 'PATCH',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				auto_refresh: settings.autoRefresh,
				refresh_interval: settings.refreshInterval,
				project: settings.project
			})
		});

		if (!response.ok) {
			const error = await response.json().catch(() => ({ message: response.statusText }));
			throw new Error(error.message || `Update failed: ${response.status}`);
		}

		return response.json();
	},

	/**
	 * Get chunks for a web source (standalone method for lazy loading).
	 */
	getWebChunks: async (sourceId: string): Promise<WebChunk[]> => {
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
			.map((raw) => transformToWebChunk(raw, sourceId))
			.filter((c): c is WebChunk => c !== null)
			.sort((a, b) => a.index - b.index);
	}
};
