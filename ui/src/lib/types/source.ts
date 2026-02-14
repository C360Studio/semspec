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

/**
 * Language metadata for display.
 */
export const LANGUAGE_META: Record<string, { name: string; color: string }> = {
	go: { name: 'Go', color: '#00ADD8' },
	typescript: { name: 'TypeScript', color: '#3178C6' },
	javascript: { name: 'JavaScript', color: '#F7DF1E' },
	python: { name: 'Python', color: '#3776AB' },
	rust: { name: 'Rust', color: '#DEA584' },
	java: { name: 'Java', color: '#B07219' },
	kotlin: { name: 'Kotlin', color: '#A97BFF' },
	swift: { name: 'Swift', color: '#F05138' },
	csharp: { name: 'C#', color: '#512BD4' },
	cpp: { name: 'C++', color: '#F34B7D' },
	c: { name: 'C', color: '#555555' },
	ruby: { name: 'Ruby', color: '#CC342D' },
	php: { name: 'PHP', color: '#777BB4' },
	svelte: { name: 'Svelte', color: '#FF3E00' },
	vue: { name: 'Vue', color: '#41B883' },
	html: { name: 'HTML', color: '#E34C26' },
	css: { name: 'CSS', color: '#563D7C' },
	sql: { name: 'SQL', color: '#E38C00' },
	shell: { name: 'Shell', color: '#89E051' },
	yaml: { name: 'YAML', color: '#CB171E' },
	json: { name: 'JSON', color: '#292929' },
	markdown: { name: 'Markdown', color: '#083FA1' }
};

/**
 * Pull interval options for repository settings.
 */
export const PULL_INTERVAL_OPTIONS = [
	{ value: '', label: 'Manual only' },
	{ value: '15m', label: 'Every 15 minutes' },
	{ value: '30m', label: 'Every 30 minutes' },
	{ value: '1h', label: 'Every hour' },
	{ value: '6h', label: 'Every 6 hours' },
	{ value: '12h', label: 'Every 12 hours' },
	{ value: '24h', label: 'Daily' }
];

/**
 * Request to add a new repository.
 */
export interface AddRepositoryRequest {
	url: string;
	branch?: string;
	project?: string;
	autoPull?: boolean;
	pullInterval?: string;
}

/**
 * Request to update repository settings.
 */
export interface UpdateRepositoryRequest {
	autoPull?: boolean;
	pullInterval?: string;
	project?: string;
}

/**
 * Repository detail with additional metadata.
 */
export interface RepositoryWithDetail extends RepositorySource {
	/** Code entities from this repository */
	entities?: RepositoryEntity[];
}

/**
 * A code entity from a repository.
 */
export interface RepositoryEntity {
	id: string;
	type: string;
	name: string;
	path: string;
}
