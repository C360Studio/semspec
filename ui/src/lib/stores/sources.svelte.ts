import { sourcesApi } from '$lib/api/sources';
import type {
	DocumentSource,
	RepositorySource,
	WebSource,
	SourceWithDetail,
	RepositoryWithDetail,
	WebSourceWithDetail,
	DocCategory,
	SourceStatus,
	SourceType,
	AddRepositoryRequest,
	AddWebSourceRequest
} from '$lib/types/source';

/**
 * Store for managing sources (documents, repositories, and web sources).
 * Handles listing, filtering, uploading, and deletion.
 */
class SourcesStore {
	// Document sources
	documents = $state<DocumentSource[]>([]);

	// Repository sources
	repositories = $state<RepositorySource[]>([]);

	// Web sources
	webSources = $state<WebSource[]>([]);

	// Loading and error state
	loading = $state(false);
	error = $state<string | null>(null);

	// Filtering
	selectedType = $state<SourceType | ''>('');
	selectedCategory = $state<DocCategory | ''>('');
	searchQuery = $state('');

	// Upload state
	uploading = $state(false);
	uploadProgress = $state(0);

	// Reindex state
	reindexingIds = $state<Set<string>>(new Set());

	// Pull state for repositories
	pullingIds = $state<Set<string>>(new Set());

	// Refresh state for web sources
	refreshingIds = $state<Set<string>>(new Set());

	/**
	 * All sources combined.
	 */
	get all(): (DocumentSource | RepositorySource | WebSource)[] {
		return [...this.documents, ...this.repositories, ...this.webSources];
	}

	/**
	 * Filter sources by type, category, and search query.
	 */
	get filtered(): (DocumentSource | RepositorySource | WebSource)[] {
		let sources: (DocumentSource | RepositorySource | WebSource)[] = [];

		// Filter by type
		if (this.selectedType === 'document' || this.selectedType === '') {
			sources = [...sources, ...this.documents];
		}
		if (this.selectedType === 'repository' || this.selectedType === '') {
			sources = [...sources, ...this.repositories];
		}
		if (this.selectedType === 'web' || this.selectedType === '') {
			sources = [...sources, ...this.webSources];
		}

		// Filter by category (documents only)
		if (this.selectedCategory) {
			sources = sources.filter(
				(s) => s.type !== 'document' || s.category === this.selectedCategory
			);
		}

		// Apply search filter
		if (this.searchQuery) {
			const q = this.searchQuery.toLowerCase();
			sources = sources.filter((s) => {
				if (s.type === 'document') {
					return (
						s.name.toLowerCase().includes(q) ||
						s.filename.toLowerCase().includes(q) ||
						s.summary?.toLowerCase().includes(q)
					);
				} else if (s.type === 'repository') {
					return (
						s.name.toLowerCase().includes(q) ||
						s.url.toLowerCase().includes(q) ||
						s.languages?.some((l) => l.toLowerCase().includes(q))
					);
				} else {
					return (
						s.name.toLowerCase().includes(q) ||
						s.url.toLowerCase().includes(q) ||
						s.title?.toLowerCase().includes(q)
					);
				}
			});
		}

		// Sort by addedAt descending
		sources.sort((a, b) => new Date(b.addedAt).getTime() - new Date(a.addedAt).getTime());

		return sources;
	}

	/**
	 * Filtered documents only.
	 */
	get filteredDocuments(): DocumentSource[] {
		return this.filtered.filter((s): s is DocumentSource => s.type === 'document');
	}

	/**
	 * Filtered repositories only.
	 */
	get filteredRepositories(): RepositorySource[] {
		return this.filtered.filter((s): s is RepositorySource => s.type === 'repository');
	}

	/**
	 * Filtered web sources only.
	 */
	get filteredWebSources(): WebSource[] {
		return this.filtered.filter((s): s is WebSource => s.type === 'web');
	}

	/**
	 * Group documents by category.
	 */
	get byCategory(): Record<DocCategory, DocumentSource[]> {
		const grouped: Record<DocCategory, DocumentSource[]> = {
			sop: [],
			spec: [],
			datasheet: [],
			reference: [],
			api: []
		};

		for (const source of this.documents) {
			grouped[source.category].push(source);
		}

		return grouped;
	}

	/**
	 * Count of all sources by status.
	 */
	get byStatus(): Record<SourceStatus, number> {
		const counts: Record<SourceStatus, number> = {
			pending: 0,
			indexing: 0,
			ready: 0,
			error: 0,
			stale: 0
		};

		for (const source of this.all) {
			counts[source.status]++;
		}

		return counts;
	}

	/**
	 * Total count of all sources.
	 */
	get total(): number {
		return this.documents.length + this.repositories.length + this.webSources.length;
	}

	/**
	 * Count of documents.
	 */
	get documentCount(): number {
		return this.documents.length;
	}

	/**
	 * Count of repositories.
	 */
	get repositoryCount(): number {
		return this.repositories.length;
	}

	/**
	 * Count of web sources.
	 */
	get webSourceCount(): number {
		return this.webSources.length;
	}

	/**
	 * Count of ready sources.
	 */
	get readyCount(): number {
		return this.all.filter((s) => s.status === 'ready').length;
	}

	/**
	 * Check if a source is currently being reindexed.
	 */
	isReindexing(id: string): boolean {
		return this.reindexingIds.has(id);
	}

	/**
	 * Check if a repository is currently being pulled.
	 */
	isPulling(id: string): boolean {
		return this.pullingIds.has(id);
	}

	/**
	 * Check if a web source is currently being refreshed.
	 */
	isRefreshing(id: string): boolean {
		return this.refreshingIds.has(id);
	}

	/**
	 * Fetch all sources from the API.
	 */
	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			// Fetch all source types in parallel
			const [docs, repos, web] = await Promise.all([
				sourcesApi.list(),
				sourcesApi.listRepos(),
				sourcesApi.listWeb()
			]);

			this.documents = docs;
			this.repositories = repos;
			this.webSources = web;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch sources';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Fetch only documents.
	 */
	async fetchDocuments(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.documents = await sourcesApi.list();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch documents';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Fetch only repositories.
	 */
	async fetchRepositories(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.repositories = await sourcesApi.listRepos();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch repositories';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Fetch only web sources.
	 */
	async fetchWebSources(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.webSources = await sourcesApi.listWeb();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch web sources';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Get a single document source by ID with full details.
	 */
	async getDocument(id: string): Promise<SourceWithDetail> {
		return sourcesApi.get(id);
	}

	/**
	 * Get a single repository by ID with full details.
	 */
	async getRepository(id: string): Promise<RepositoryWithDetail> {
		return sourcesApi.getRepo(id);
	}

	/**
	 * Get a single web source by ID with full details.
	 */
	async getWebSource(id: string): Promise<WebSourceWithDetail> {
		return sourcesApi.getWeb(id);
	}

	/**
	 * Upload a new document.
	 */
	async upload(
		file: File,
		options?: { project?: string; category?: DocCategory }
	): Promise<DocumentSource | null> {
		this.uploading = true;
		this.uploadProgress = 0;
		this.error = null;

		try {
			const progressInterval = setInterval(() => {
				if (this.uploadProgress < 90) {
					this.uploadProgress += 10;
				}
			}, 100);

			const response = await sourcesApi.upload(file, options);

			clearInterval(progressInterval);
			this.uploadProgress = 100;

			const newSource: DocumentSource = {
				id: response.id,
				type: 'document',
				name: file.name.replace(/\.[^/.]+$/, ''),
				status: response.status,
				addedAt: new Date().toISOString(),
				filename: file.name,
				mimeType: file.type || 'text/plain',
				category: options?.category || 'reference',
				project: options?.project
			};

			this.documents = [newSource, ...this.documents];

			return newSource;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Upload failed';
			return null;
		} finally {
			this.uploading = false;
			setTimeout(() => {
				this.uploadProgress = 0;
			}, 500);
		}
	}

	/**
	 * Add a new repository.
	 */
	async addRepository(request: AddRepositoryRequest): Promise<RepositorySource | null> {
		this.error = null;

		try {
			const response = await sourcesApi.addRepo(request);

			// Extract name from URL
			const urlParts = request.url.replace(/\.git$/, '').split('/');
			const name = urlParts[urlParts.length - 1] || 'repository';

			const newRepo: RepositorySource = {
				id: response.id,
				type: 'repository',
				name,
				status: 'pending',
				addedAt: new Date().toISOString(),
				url: request.url,
				branch: request.branch || 'main',
				autoPull: request.autoPull,
				pullInterval: request.pullInterval,
				project: request.project
			};

			this.repositories = [newRepo, ...this.repositories];

			return newRepo;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Add repository failed';
			return null;
		}
	}

	/**
	 * Add a new web source.
	 */
	async addWebSource(request: AddWebSourceRequest): Promise<WebSource | null> {
		this.error = null;

		try {
			const response = await sourcesApi.addWeb(request);

			// Extract name from URL
			let name = request.url;
			try {
				const parsed = new URL(request.url);
				name = parsed.hostname + (parsed.pathname !== '/' ? parsed.pathname : '');
			} catch {
				// Keep full URL as name if parsing fails
			}

			const newSource: WebSource = {
				id: response.id,
				type: 'web',
				name,
				title: response.title,
				status: 'pending',
				addedAt: new Date().toISOString(),
				url: request.url,
				autoRefresh: request.autoRefresh,
				refreshInterval: request.refreshInterval,
				project: request.project
			};

			this.webSources = [newSource, ...this.webSources];

			return newSource;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Add web source failed';
			return null;
		}
	}

	/**
	 * Delete a document source.
	 */
	async deleteDocument(id: string): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.delete(id);
			this.documents = this.documents.filter((s) => s.id !== id);
			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Delete failed';
			return false;
		}
	}

	/**
	 * Delete a repository.
	 */
	async deleteRepository(id: string): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.deleteRepo(id);
			this.repositories = this.repositories.filter((r) => r.id !== id);
			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Delete failed';
			return false;
		}
	}

	/**
	 * Delete a web source.
	 */
	async deleteWebSource(id: string): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.deleteWeb(id);
			this.webSources = this.webSources.filter((w) => w.id !== id);
			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Delete failed';
			return false;
		}
	}

	/**
	 * Request reindexing of a document.
	 */
	async reindexDocument(id: string): Promise<boolean> {
		this.error = null;
		this.reindexingIds = new Set([...this.reindexingIds, id]);

		try {
			const response = await sourcesApi.reindex(id);

			const idx = this.documents.findIndex((s) => s.id === id);
			if (idx !== -1) {
				this.documents[idx] = { ...this.documents[idx], status: response.status };
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Reindex failed';
			return false;
		} finally {
			const newSet = new Set(this.reindexingIds);
			newSet.delete(id);
			this.reindexingIds = newSet;
		}
	}

	/**
	 * Request reindexing of a repository.
	 */
	async reindexRepository(id: string): Promise<boolean> {
		this.error = null;
		this.reindexingIds = new Set([...this.reindexingIds, id]);

		try {
			const response = await sourcesApi.reindexRepo(id);

			const idx = this.repositories.findIndex((r) => r.id === id);
			if (idx !== -1) {
				this.repositories[idx] = { ...this.repositories[idx], status: response.status as SourceStatus };
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Reindex failed';
			return false;
		} finally {
			const newSet = new Set(this.reindexingIds);
			newSet.delete(id);
			this.reindexingIds = newSet;
		}
	}

	/**
	 * Pull updates for a repository.
	 */
	async pullRepository(id: string): Promise<boolean> {
		this.error = null;
		this.pullingIds = new Set([...this.pullingIds, id]);

		try {
			const response = await sourcesApi.pullRepo(id);

			const idx = this.repositories.findIndex((r) => r.id === id);
			if (idx !== -1) {
				this.repositories[idx] = {
					...this.repositories[idx],
					status: response.status as SourceStatus,
					lastCommit: response.lastCommit
				};
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Pull failed';
			return false;
		} finally {
			const newSet = new Set(this.pullingIds);
			newSet.delete(id);
			this.pullingIds = newSet;
		}
	}

	/**
	 * Update repository settings.
	 */
	async updateRepository(
		id: string,
		settings: { autoPull?: boolean; pullInterval?: string; project?: string }
	): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.updateRepo(id, settings);

			const idx = this.repositories.findIndex((r) => r.id === id);
			if (idx !== -1) {
				this.repositories[idx] = {
					...this.repositories[idx],
					...settings
				};
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Update failed';
			return false;
		}
	}

	/**
	 * Refresh a web source.
	 */
	async refreshWebSource(id: string): Promise<boolean> {
		this.error = null;
		this.refreshingIds = new Set([...this.refreshingIds, id]);

		try {
			const response = await sourcesApi.refreshWeb(id);

			const idx = this.webSources.findIndex((w) => w.id === id);
			if (idx !== -1) {
				this.webSources[idx] = {
					...this.webSources[idx],
					status: response.status,
					contentHash: response.contentHash,
					lastFetched: new Date().toISOString()
				};
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Refresh failed';
			return false;
		} finally {
			const newSet = new Set(this.refreshingIds);
			newSet.delete(id);
			this.refreshingIds = newSet;
		}
	}

	/**
	 * Update web source settings.
	 */
	async updateWebSource(
		id: string,
		settings: { autoRefresh?: boolean; refreshInterval?: string; project?: string }
	): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.updateWeb(id, settings);

			const idx = this.webSources.findIndex((w) => w.id === id);
			if (idx !== -1) {
				this.webSources[idx] = {
					...this.webSources[idx],
					...settings
				};
			}

			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Update failed';
			return false;
		}
	}

	/**
	 * Update filter type.
	 */
	setType(type: SourceType | ''): void {
		this.selectedType = type;
	}

	/**
	 * Update filter category.
	 */
	setCategory(category: DocCategory | ''): void {
		this.selectedCategory = category;
	}

	/**
	 * Update search query.
	 */
	setSearch(query: string): void {
		this.searchQuery = query;
	}

	/**
	 * Clear all filters.
	 */
	clearFilters(): void {
		this.selectedType = '';
		this.selectedCategory = '';
		this.searchQuery = '';
	}

	/**
	 * Clear error state.
	 */
	clearError(): void {
		this.error = null;
	}
}

export const sourcesStore = new SourcesStore();
