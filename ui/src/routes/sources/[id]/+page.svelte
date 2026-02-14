<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import type { SourceWithDetail, RepositoryWithDetail, DocumentChunk } from '$lib/types/source';
	import { CATEGORY_META, STATUS_META, LANGUAGE_META, PULL_INTERVAL_OPTIONS } from '$lib/types/source';

	type SourceDetail = SourceWithDetail | RepositoryWithDetail;

	let source = $state<SourceDetail | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let expandedChunks = $state<Set<number>>(new Set());
	let confirmDelete = $state(false);

	const sourceId = $derived($page.params.id ?? '');
	const isDocument = $derived(source?.type === 'document');
	const isRepository = $derived(source?.type === 'repository');

	const categoryMeta = $derived(
		isDocument && 'category' in source! ? CATEGORY_META[source.category] : null
	);
	const statusMeta = $derived(source ? STATUS_META[source.status] : null);
	const isReindexing = $derived(source ? sourcesStore.isReindexing(source.id) : false);
	const isPulling = $derived(source ? sourcesStore.isPulling(source.id) : false);

	async function loadSource() {
		loading = true;
		error = null;

		try {
			// Determine type from ID prefix
			if (sourceId.startsWith('source.repo.')) {
				source = await sourcesStore.getRepository(sourceId);
			} else {
				source = await sourcesStore.getDocument(sourceId);
			}
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load source';
		} finally {
			loading = false;
		}
	}

	async function handleReindex() {
		if (!source) return;
		let success: boolean;
		if (isDocument) {
			success = await sourcesStore.reindexDocument(source.id);
		} else {
			success = await sourcesStore.reindexRepository(source.id);
		}
		if (success) {
			await loadSource();
		}
	}

	async function handlePull() {
		if (!source || !isRepository) return;
		const success = await sourcesStore.pullRepository(source.id);
		if (success) {
			await loadSource();
		}
	}

	async function handleDelete() {
		if (!source || !confirmDelete) return;
		let success: boolean;
		if (isDocument) {
			success = await sourcesStore.deleteDocument(source.id);
		} else {
			success = await sourcesStore.deleteRepository(source.id);
		}
		if (success) {
			goto('/sources');
		}
	}

	function toggleChunk(index: number) {
		const newSet = new Set(expandedChunks);
		if (newSet.has(index)) {
			newSet.delete(index);
		} else {
			newSet.add(index);
		}
		expandedChunks = newSet;
	}

	function expandAllChunks() {
		if (isDocument && 'chunks' in source! && source.chunks) {
			expandedChunks = new Set(source.chunks.map((c: DocumentChunk) => c.index));
		}
	}

	function collapseAllChunks() {
		expandedChunks = new Set();
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleString();
	}

	function formatCommit(commit: string): string {
		return commit.substring(0, 7);
	}

	function getLanguageColor(lang: string): string {
		return LANGUAGE_META[lang]?.color || 'var(--color-text-muted)';
	}

	function getLanguageName(lang: string): string {
		return LANGUAGE_META[lang]?.name || lang;
	}

	function getPullIntervalLabel(interval: string): string {
		const option = PULL_INTERVAL_OPTIONS.find(o => o.value === interval);
		return option?.label || interval;
	}

	onMount(loadSource);
</script>

<svelte:head>
	<title>{source?.name ?? 'Source'} - Semspec</title>
</svelte:head>

<div class="source-detail-page">
	<nav class="breadcrumb">
		<a href="/sources">Sources</a>
		<Icon name="chevron-right" size={14} />
		<span>{source?.name ?? 'Loading...'}</span>
	</nav>

	{#if loading}
		<div class="loading-state">
			<Icon name="loader" size={24} />
			<span>Loading source...</span>
		</div>
	{:else if error}
		<div class="error-state">
			<Icon name="alert-circle" size={24} />
			<span>{error}</span>
			<button onclick={loadSource}>Retry</button>
		</div>
	{:else if source}
		<header class="source-header">
			<div class="header-main">
				{#if isDocument && categoryMeta}
					<div class="source-icon" style="color: {categoryMeta.color}">
						<Icon name={categoryMeta.icon} size={32} />
					</div>
				{:else if isRepository}
					<div class="source-icon" style="color: var(--color-accent)">
						<Icon name="git-branch" size={32} />
					</div>
				{/if}
				<div class="header-content">
					<h1>{source.name}</h1>
					<div class="header-badges">
						{#if isDocument && categoryMeta}
							<span
								class="category-badge"
								style="background: {categoryMeta.color}20; color: {categoryMeta.color}"
							>
								{categoryMeta.label}
							</span>
						{:else if isRepository}
							<span class="type-badge">
								<Icon name="git-branch" size={12} />
								Repository
							</span>
						{/if}
						{#if statusMeta}
							<span class="status-badge" style="color: {statusMeta.color}">
								<Icon name={statusMeta.icon} size={14} />
								{statusMeta.label}
							</span>
						{/if}
					</div>
				</div>
			</div>

			<div class="header-actions">
				{#if isRepository}
					<button
						class="btn btn-secondary"
						onclick={handlePull}
						disabled={isPulling}
					>
						{#if isPulling}
							<Icon name="loader" size={16} />
							Pulling...
						{:else}
							<Icon name="download" size={16} />
							Pull
						{/if}
					</button>
				{/if}
				<button
					class="btn btn-secondary"
					onclick={handleReindex}
					disabled={isReindexing}
				>
					{#if isReindexing}
						<Icon name="loader" size={16} />
						Reindexing...
					{:else}
						<Icon name="refresh-cw" size={16} />
						Reindex
					{/if}
				</button>
				{#if !confirmDelete}
					<button class="btn btn-danger-outline" onclick={() => (confirmDelete = true)}>
						<Icon name="trash-2" size={16} />
						Delete
					</button>
				{:else}
					<div class="confirm-delete">
						<span>Delete this source?</span>
						<button class="btn btn-danger" onclick={handleDelete}>Yes, delete</button>
						<button class="btn btn-secondary" onclick={() => (confirmDelete = false)}>Cancel</button>
					</div>
				{/if}
			</div>
		</header>

		<div class="source-content">
			<!-- Common details section -->
			<section class="metadata-section">
				<h2>Details</h2>
				<dl class="metadata-grid">
					{#if isDocument && 'filename' in source}
						<dt>Filename</dt>
						<dd class="mono">{source.filename}</dd>

						<dt>MIME Type</dt>
						<dd class="mono">{source.mimeType}</dd>

						{#if source.filePath}
							<dt>File Path</dt>
							<dd class="mono">{source.filePath}</dd>
						{/if}
					{/if}

					{#if isRepository && 'url' in source}
						<dt>URL</dt>
						<dd class="mono">{source.url}</dd>

						<dt>Branch</dt>
						<dd class="mono">{source.branch}</dd>

						{#if source.lastCommit}
							<dt>Last Commit</dt>
							<dd class="mono">{formatCommit(source.lastCommit)}</dd>
						{/if}

						{#if source.lastIndexed}
							<dt>Last Indexed</dt>
							<dd>{formatDate(source.lastIndexed)}</dd>
						{/if}

						{#if source.entityCount !== undefined}
							<dt>Entities</dt>
							<dd>{source.entityCount}</dd>
						{/if}

						<dt>Auto-Pull</dt>
						<dd>{source.autoPull ? 'Enabled' : 'Disabled'}</dd>

						{#if source.autoPull && source.pullInterval}
							<dt>Pull Interval</dt>
							<dd>{getPullIntervalLabel(source.pullInterval)}</dd>
						{/if}
					{/if}

					<dt>Added</dt>
					<dd>{formatDate(source.addedAt)}</dd>

					{#if source.addedBy}
						<dt>Added By</dt>
						<dd>{source.addedBy}</dd>
					{/if}

					{#if source.project}
						<dt>Project</dt>
						<dd>{source.project}</dd>
					{/if}

					{#if isDocument && 'severity' in source && source.severity}
						<dt>Severity</dt>
						<dd class="severity-{source.severity}">{source.severity}</dd>
					{/if}

					{#if isDocument && 'chunkCount' in source && source.chunkCount !== undefined}
						<dt>Chunks</dt>
						<dd>{source.chunkCount}</dd>
					{/if}
				</dl>
			</section>

			<!-- Languages section for repositories -->
			{#if isRepository && 'languages' in source && source.languages && source.languages.length > 0}
				<section class="languages-section">
					<h2>Languages</h2>
					<div class="languages">
						{#each source.languages as lang}
							<span
								class="language-badge"
								style="background: {getLanguageColor(lang)}20; color: {getLanguageColor(lang)}"
							>
								{getLanguageName(lang)}
							</span>
						{/each}
					</div>
				</section>
			{/if}

			<!-- Summary section for documents -->
			{#if isDocument && 'summary' in source && source.summary}
				<section class="summary-section">
					<h2>Summary</h2>
					<p>{source.summary}</p>
				</section>
			{/if}

			<!-- Applies to section for documents -->
			{#if isDocument && 'appliesTo' in source && source.appliesTo && source.appliesTo.length > 0}
				<section class="applies-to-section">
					<h2>Applies To</h2>
					<ul class="pattern-list">
						{#each source.appliesTo as pattern}
							<li class="mono">{pattern}</li>
						{/each}
					</ul>
				</section>
			{/if}

			<!-- Requirements section for documents -->
			{#if isDocument && 'requirements' in source && source.requirements && source.requirements.length > 0}
				<section class="requirements-section">
					<h2>Requirements</h2>
					<ul class="requirements-list">
						{#each source.requirements as req}
							<li>{req}</li>
						{/each}
					</ul>
				</section>
			{/if}

			<!-- Chunks section for documents -->
			{#if isDocument && 'chunks' in source && source.chunks && source.chunks.length > 0}
				<section class="chunks-section">
					<div class="chunks-header">
						<h2>Chunks ({source.chunks.length})</h2>
						<div class="chunks-actions">
							<button class="link-button" onclick={expandAllChunks}>Expand all</button>
							<button class="link-button" onclick={collapseAllChunks}>Collapse all</button>
						</div>
					</div>

					<div class="chunks-list">
						{#each source.chunks as chunk (chunk.id)}
							<div class="chunk-item" class:expanded={expandedChunks.has(chunk.index)}>
								<button
									class="chunk-header"
									onclick={() => toggleChunk(chunk.index)}
									aria-expanded={expandedChunks.has(chunk.index)}
								>
									<Icon
										name={expandedChunks.has(chunk.index) ? 'chevron-down' : 'chevron-right'}
										size={16}
									/>
									<span class="chunk-index">Chunk {chunk.index}</span>
									{#if chunk.section}
										<span class="chunk-section">{chunk.section}</span>
									{/if}
								</button>
								{#if expandedChunks.has(chunk.index)}
									<div class="chunk-content">
										<pre>{chunk.content}</pre>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				</section>
			{/if}

			<!-- Error section -->
			{#if source.error}
				<section class="error-section">
					<h2>Error</h2>
					<div class="error-message">
						<Icon name="alert-circle" size={16} />
						<span>{source.error}</span>
					</div>
				</section>
			{/if}
		</div>
	{/if}
</div>

<style>
	.source-detail-page {
		max-width: 1000px;
		margin: 0 auto;
		padding: var(--space-6);
	}

	.breadcrumb {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-4);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.breadcrumb a {
		color: var(--color-accent);
		text-decoration: none;
	}

	.breadcrumb a:hover {
		text-decoration: underline;
	}

	.loading-state,
	.error-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-12);
		text-align: center;
		color: var(--color-text-muted);
	}

	.error-state {
		color: var(--color-error);
	}

	.error-state button {
		margin-top: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-error);
		color: white;
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
	}

	.source-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: var(--space-4);
		margin-bottom: var(--space-6);
		flex-wrap: wrap;
	}

	.header-main {
		display: flex;
		align-items: flex-start;
		gap: var(--space-4);
	}

	.source-icon {
		flex-shrink: 0;
	}

	.header-content h1 {
		margin: 0;
		font-size: var(--font-size-2xl);
		color: var(--color-text-primary);
	}

	.header-badges {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}

	.category-badge {
		font-size: var(--font-size-xs);
		padding: 2px 8px;
		border-radius: var(--radius-sm);
		text-transform: uppercase;
		font-weight: var(--font-weight-medium);
	}

	.type-badge {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		padding: 2px 8px;
		border-radius: var(--radius-sm);
		background: var(--color-accent-muted);
		color: var(--color-accent);
		text-transform: uppercase;
		font-weight: var(--font-weight-medium);
	}

	.status-badge {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-sm);
	}

	.header-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-secondary {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
	}

	.btn-secondary:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-danger-outline {
		background: none;
		border: 1px solid var(--color-error);
		color: var(--color-error);
	}

	.btn-danger-outline:hover {
		background: var(--color-error);
		color: white;
	}

	.btn-danger {
		background: var(--color-error);
		border: none;
		color: white;
	}

	.confirm-delete {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
	}

	.confirm-delete span {
		color: var(--color-error);
	}

	.source-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
	}

	section {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-4);
	}

	section h2 {
		margin: 0 0 var(--space-3);
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.metadata-grid {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-2) var(--space-4);
		margin: 0;
	}

	.metadata-grid dt {
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.metadata-grid dd {
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		margin: 0;
	}

	.mono {
		font-family: var(--font-mono);
	}

	.severity-error {
		color: var(--color-error);
	}

	.severity-warning {
		color: var(--color-warning);
	}

	.severity-info {
		color: var(--color-info);
	}

	.languages {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.language-badge {
		font-size: var(--font-size-sm);
		padding: 4px 10px;
		border-radius: var(--radius-sm);
		font-weight: var(--font-weight-medium);
	}

	.summary-section p {
		margin: 0;
		line-height: 1.6;
		color: var(--color-text-secondary);
	}

	.pattern-list,
	.requirements-list {
		margin: 0;
		padding-left: var(--space-4);
	}

	.pattern-list li,
	.requirements-list li {
		margin-bottom: var(--space-1);
		color: var(--color-text-secondary);
	}

	.chunks-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-3);
	}

	.chunks-header h2 {
		margin: 0;
	}

	.chunks-actions {
		display: flex;
		gap: var(--space-3);
	}

	.link-button {
		background: none;
		border: none;
		color: var(--color-accent);
		font-size: var(--font-size-sm);
		cursor: pointer;
		padding: 0;
	}

	.link-button:hover {
		text-decoration: underline;
	}

	.chunks-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.chunk-item {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		overflow: hidden;
	}

	.chunk-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: none;
		text-align: left;
		cursor: pointer;
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.chunk-header:hover {
		background: var(--color-bg-primary);
	}

	.chunk-index {
		font-weight: var(--font-weight-medium);
	}

	.chunk-section {
		color: var(--color-text-muted);
		margin-left: auto;
	}

	.chunk-content {
		padding: var(--space-3);
		background: var(--color-bg-primary);
		border-top: 1px solid var(--color-border);
	}

	.chunk-content pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: var(--font-size-sm);
		white-space: pre-wrap;
		word-break: break-word;
		line-height: 1.5;
		color: var(--color-text-secondary);
	}

	.error-section {
		border-color: var(--color-error);
	}

	.error-message {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		color: var(--color-error);
	}
</style>
