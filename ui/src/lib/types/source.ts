/**
 * Types for the Sources management system.
 * Sources are documents and repositories that provide context for development.
 */

/**
 * Source type discriminator.
 */
export type SourceType = 'document' | 'repository';

/**
 * Source status indicating indexing state.
 */
export type SourceStatus = 'pending' | 'indexing' | 'ready' | 'error' | 'stale';

/**
 * Document category for classification.
 */
export type DocCategory = 'sop' | 'spec' | 'datasheet' | 'reference' | 'api';

/**
 * Base source interface shared by documents and repositories.
 */
export interface Source {
	/** Entity ID (e.g., source.doc.auth-sop) */
	id: string;
	/** Display name */
	name: string;
	/** Source type discriminator */
	type: SourceType;
	/** Current indexing status */
	status: SourceStatus;
	/** When the source was added (RFC3339) */
	addedAt: string;
	/** User/agent who added this source */
	addedBy?: string;
	/** Project tag for grouping */
	project?: string;
	/** Error message if status is 'error' */
	error?: string;
}

/**
 * Document source with document-specific metadata.
 */
export interface DocumentSource extends Source {
	type: 'document';
	/** Original filename */
	filename: string;
	/** MIME type (e.g., text/markdown, application/pdf) */
	mimeType: string;
	/** Document category */
	category: DocCategory;
	/** LLM-extracted summary */
	summary?: string;
	/** Extracted requirements for review validation */
	requirements?: string[];
	/** Number of chunks in the document */
	chunkCount?: number;
	/** File patterns this document applies to */
	appliesTo?: string[];
	/** Violation severity for SOPs */
	severity?: 'error' | 'warning' | 'info';
	/** File path in .semspec/sources/docs/ */
	filePath?: string;
}

/**
 * Repository source with repository-specific metadata.
 */
export interface RepositorySource extends Source {
	type: 'repository';
	/** Git clone URL */
	url: string;
	/** Branch being tracked */
	branch: string;
	/** Programming languages detected */
	languages?: string[];
	/** Number of entities indexed */
	entityCount?: number;
	/** Last successful indexing timestamp */
	lastIndexed?: string;
	/** Whether auto-pull is enabled */
	autoPull?: boolean;
	/** Auto-pull interval (duration string) */
	pullInterval?: string;
	/** SHA of last indexed commit */
	lastCommit?: string;
}

/**
 * Document chunk representing a portion of a document.
 */
export interface DocumentChunk {
	/** Chunk entity ID */
	id: string;
	/** Parent document ID */
	parentId: string;
	/** Chunk index (1-indexed) */
	index: number;
	/** Section/heading name */
	section?: string;
	/** Chunk text content */
	content: string;
}

/**
 * Source with full detail including chunks (for detail page).
 */
export interface SourceWithDetail extends DocumentSource {
	/** Document chunks */
	chunks?: DocumentChunk[];
}

/**
 * Upload response from the server.
 */
export interface UploadResponse {
	/** Created source entity ID */
	id: string;
	/** Initial status (usually 'pending') */
	status: SourceStatus;
	/** Message from server */
	message?: string;
}

/**
 * Reindex response from the server.
 */
export interface ReindexResponse {
	/** Source ID */
	id: string;
	/** New status (usually 'pending' or 'indexing') */
	status: SourceStatus;
	/** Message from server */
	message?: string;
}

/**
 * Category metadata for display.
 */
export const CATEGORY_META: Record<DocCategory, { label: string; color: string; icon: string }> = {
	sop: { label: 'SOP', color: 'var(--color-warning)', icon: 'alert-triangle' },
	spec: { label: 'Spec', color: 'var(--color-success)', icon: 'file-text' },
	datasheet: { label: 'Datasheet', color: 'var(--color-info)', icon: 'database' },
	reference: { label: 'Reference', color: 'var(--color-accent)', icon: 'book-open' },
	api: { label: 'API', color: 'var(--color-secondary)', icon: 'code' }
};

/**
 * Status metadata for display.
 */
export const STATUS_META: Record<SourceStatus, { label: string; color: string; icon: string }> = {
	pending: { label: 'Pending', color: 'var(--color-text-muted)', icon: 'clock' },
	indexing: { label: 'Indexing', color: 'var(--color-warning)', icon: 'loader' },
	ready: { label: 'Ready', color: 'var(--color-success)', icon: 'check-circle' },
	error: { label: 'Error', color: 'var(--color-error)', icon: 'alert-circle' },
	stale: { label: 'Stale', color: 'var(--color-warning)', icon: 'alert-triangle' }
};
