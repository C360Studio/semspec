import { sourcesApi } from '$lib/api/sources';
import type {
	DocumentSource,
	SourceWithDetail,
	DocCategory,
	SourceStatus
} from '$lib/types/source';

/**
 * Store for managing document sources.
 * Handles listing, filtering, uploading, and deletion.
 */
class SourcesStore {
	all = $state<DocumentSource[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);
	selectedCategory = $state<DocCategory | ''>('');
	searchQuery = $state('');

	// Upload state
	uploading = $state(false);
	uploadProgress = $state(0);

	// Reindex state
	reindexingIds = $state<Set<string>>(new Set());

	/**
	 * Filter sources by current category and search query.
	 */
	get filtered(): DocumentSource[] {
		let sources = this.all;

		if (this.selectedCategory) {
			sources = sources.filter((s) => s.category === this.selectedCategory);
		}

		if (this.searchQuery) {
			const q = this.searchQuery.toLowerCase();
			sources = sources.filter(
				(s) =>
					s.name.toLowerCase().includes(q) ||
					s.filename.toLowerCase().includes(q) ||
					s.summary?.toLowerCase().includes(q)
			);
		}

		return sources;
	}

	/**
	 * Group sources by category.
	 */
	get byCategory(): Record<DocCategory, DocumentSource[]> {
		const grouped: Record<DocCategory, DocumentSource[]> = {
			sop: [],
			spec: [],
			datasheet: [],
			reference: [],
			api: []
		};

		for (const source of this.all) {
			grouped[source.category].push(source);
		}

		return grouped;
	}

	/**
	 * Count of sources by status.
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
	 * Total count of sources.
	 */
	get total(): number {
		return this.all.length;
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
	 * Fetch all sources from the API.
	 */
	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.all = await sourcesApi.list();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch sources';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Get a single source by ID with full details.
	 */
	async get(id: string): Promise<SourceWithDetail> {
		return sourcesApi.get(id);
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
			// Simulate progress (actual progress would need XHR)
			const progressInterval = setInterval(() => {
				if (this.uploadProgress < 90) {
					this.uploadProgress += 10;
				}
			}, 100);

			const response = await sourcesApi.upload(file, options);

			clearInterval(progressInterval);
			this.uploadProgress = 100;

			// Create optimistic source entry
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

			this.all = [newSource, ...this.all];

			return newSource;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Upload failed';
			return null;
		} finally {
			this.uploading = false;
			// Reset progress after a delay
			setTimeout(() => {
				this.uploadProgress = 0;
			}, 500);
		}
	}

	/**
	 * Delete a document source.
	 */
	async delete(id: string): Promise<boolean> {
		this.error = null;

		try {
			await sourcesApi.delete(id);
			this.all = this.all.filter((s) => s.id !== id);
			return true;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Delete failed';
			return false;
		}
	}

	/**
	 * Request reindexing of a document.
	 */
	async reindex(id: string): Promise<boolean> {
		this.error = null;
		this.reindexingIds = new Set([...this.reindexingIds, id]);

		try {
			const response = await sourcesApi.reindex(id);

			// Update local state
			const idx = this.all.findIndex((s) => s.id === id);
			if (idx !== -1) {
				this.all[idx] = { ...this.all[idx], status: response.status };
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
