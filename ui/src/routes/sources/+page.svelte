<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import SourceCard from '$lib/components/sources/SourceCard.svelte';
	import RepositoryCard from '$lib/components/sources/RepositoryCard.svelte';
	import WebSourceCard from '$lib/components/sources/WebSourceCard.svelte';
	import CategoryFilter from '$lib/components/sources/CategoryFilter.svelte';
	import TypeFilter from '$lib/components/sources/TypeFilter.svelte';
	import UploadModal from '$lib/components/sources/UploadModal.svelte';
	import AddRepositoryModal from '$lib/components/sources/AddRepositoryModal.svelte';
	import AddWebSourceModal from '$lib/components/sources/AddWebSourceModal.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import type { DocCategory, SourceType, AddRepositoryRequest, AddWebSourceRequest } from '$lib/types/source';

	let showUploadModal = $state(false);
	let showAddRepoModal = $state(false);
	let showAddWebModal = $state(false);
	let addingRepo = $state(false);
	let addingWeb = $state(false);
	let searchTimeout: ReturnType<typeof setTimeout>;

	// Clean up search timeout on unmount
	$effect(() => {
		return () => {
			if (searchTimeout) {
				clearTimeout(searchTimeout);
			}
		};
	});

	// Reactive getters from store
	const loading = $derived(sourcesStore.loading);
	const error = $derived(sourcesStore.error);
	const filtered = $derived(sourcesStore.filtered);
	const byCategory = $derived(sourcesStore.byCategory);
	const total = $derived(sourcesStore.total);
	const documentCount = $derived(sourcesStore.documentCount);
	const repositoryCount = $derived(sourcesStore.repositoryCount);
	const webSourceCount = $derived(sourcesStore.webSourceCount);
	const uploading = $derived(sourcesStore.uploading);
	const uploadProgress = $derived(sourcesStore.uploadProgress);
	const selectedType = $derived(sourcesStore.selectedType);

	function handleSourceClick(sourceId: string) {
		goto(`/sources/${encodeURIComponent(sourceId)}`);
	}

	function handleSearch(e: Event) {
		const target = e.target as HTMLInputElement;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(() => {
			sourcesStore.setSearch(target.value);
		}, 300);
	}

	function handleTypeChange(type: SourceType | '') {
		sourcesStore.setType(type);
	}

	function handleCategoryChange(category: DocCategory | '') {
		sourcesStore.setCategory(category);
	}

	async function handleUpload(file: File, options: { category: DocCategory; project?: string }) {
		const source = await sourcesStore.upload(file, options);
		if (source) {
			showUploadModal = false;
		}
	}

	async function handleAddRepository(request: AddRepositoryRequest) {
		addingRepo = true;
		try {
			const repo = await sourcesStore.addRepository(request);
			if (repo) {
				showAddRepoModal = false;
			}
		} finally {
			addingRepo = false;
		}
	}

	async function handleAddWebSource(request: AddWebSourceRequest) {
		addingWeb = true;
		try {
			const source = await sourcesStore.addWebSource(request);
			if (source) {
				showAddWebModal = false;
			}
		} finally {
			addingWeb = false;
		}
	}

	function getCategoryCounts(): Record<DocCategory, number> {
		const counts: Record<DocCategory, number> = {
			sop: 0,
			spec: 0,
			datasheet: 0,
			reference: 0,
			api: 0
		};
		for (const [cat, sources] of Object.entries(byCategory)) {
			counts[cat as DocCategory] = sources.length;
		}
		return counts;
	}

	onMount(() => {
		sourcesStore.fetch();
	});
</script>

<svelte:head>
	<title>Sources - Semspec</title>
</svelte:head>

<div class="sources-page">
	<header class="page-header">
		<div class="header-content">
			<h1>Sources</h1>
			<p class="subtitle">Browse and manage documents, repositories, and web sources</p>
		</div>
		<div class="header-actions">
			<button class="action-button secondary" onclick={() => (showAddWebModal = true)}>
				<Icon name="globe" size={18} />
				Add URL
			</button>
			<button class="action-button secondary" onclick={() => (showAddRepoModal = true)}>
				<Icon name="git-branch" size={18} />
				Add Repository
			</button>
			<button class="action-button primary" onclick={() => (showUploadModal = true)}>
				<Icon name="plus" size={18} />
				Upload Document
			</button>
		</div>
	</header>

	<div class="filters">
		<div class="search-box">
			<Icon name="search" size={18} />
			<input
				type="search"
				placeholder="Search sources..."
				value={sourcesStore.searchQuery}
				oninput={handleSearch}
				aria-label="Search sources"
			/>
		</div>

		<TypeFilter
			value={selectedType}
			{documentCount}
			{repositoryCount}
			{webSourceCount}
			onchange={handleTypeChange}
		/>
	</div>

	{#if selectedType !== 'repository'}
		<div class="category-filter-row">
			<CategoryFilter
				value={sourcesStore.selectedCategory}
				counts={getCategoryCounts()}
				onchange={handleCategoryChange}
			/>
		</div>
	{/if}

	{#if loading}
		<div class="loading-state">
			<Icon name="loader" size={24} />
			<span>Loading sources...</span>
		</div>
	{:else if error}
		<div class="error-state">
			<Icon name="alert-circle" size={24} />
			<span>{error}</span>
			<button onclick={() => sourcesStore.fetch()}>Retry</button>
		</div>
	{:else if filtered.length === 0}
		<div class="empty-state">
			<Icon name="file-plus" size={48} />
			<h2>No sources found</h2>
			<p>
				{#if sourcesStore.searchQuery || sourcesStore.selectedCategory || sourcesStore.selectedType}
					Try adjusting your search or filters
				{:else}
					Upload documents or add repositories to provide context for development.
				{/if}
			</p>
			{#if !sourcesStore.searchQuery && !sourcesStore.selectedCategory && !sourcesStore.selectedType}
				<div class="empty-actions">
					<button class="action-cta secondary" onclick={() => (showAddWebModal = true)}>
						<Icon name="globe" size={18} />
						Add your first URL
					</button>
					<button class="action-cta secondary" onclick={() => (showAddRepoModal = true)}>
						<Icon name="git-branch" size={18} />
						Add your first repository
					</button>
					<button class="action-cta primary" onclick={() => (showUploadModal = true)}>
						<Icon name="upload" size={18} />
						Upload your first document
					</button>
				</div>
			{/if}
		</div>
	{:else}
		<div class="sources-summary">
			<span class="count">{filtered.length} of {total} sources</span>
			{#if sourcesStore.searchQuery || sourcesStore.selectedCategory || sourcesStore.selectedType}
				<button class="clear-filters" onclick={() => sourcesStore.clearFilters()}>
					Clear filters
				</button>
			{/if}
		</div>

		<div class="source-list">
			{#each filtered as source (source.id)}
				{#if source.type === 'document'}
					<SourceCard {source} onclick={() => handleSourceClick(source.id)} />
				{:else if source.type === 'repository'}
					<RepositoryCard {source} onclick={() => handleSourceClick(source.id)} />
				{:else}
					<WebSourceCard {source} onclick={() => handleSourceClick(source.id)} />
				{/if}
			{/each}
		</div>
	{/if}
</div>

<UploadModal
	open={showUploadModal}
	{uploading}
	progress={uploadProgress}
	onclose={() => (showUploadModal = false)}
	onupload={handleUpload}
/>

<AddRepositoryModal
	open={showAddRepoModal}
	loading={addingRepo}
	onclose={() => (showAddRepoModal = false)}
	onsubmit={handleAddRepository}
/>

<AddWebSourceModal
	open={showAddWebModal}
	loading={addingWeb}
	onclose={() => (showAddWebModal = false)}
	onsubmit={handleAddWebSource}
/>

<style>
	.sources-page {
		max-width: 1200px;
		margin: 0 auto;
		padding: var(--space-6);
	}

	.page-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		margin-bottom: var(--space-6);
		gap: var(--space-4);
		flex-wrap: wrap;
	}

	.header-content h1 {
		margin: 0;
		font-size: var(--font-size-2xl);
		color: var(--color-text-primary);
	}

	.subtitle {
		margin: var(--space-1) 0 0;
		color: var(--color-text-muted);
	}

	.header-actions {
		display: flex;
		gap: var(--space-2);
	}

	.action-button {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.action-button.primary {
		background: var(--color-accent);
		border: none;
		color: white;
	}

	.action-button.primary:hover {
		opacity: 0.9;
	}

	.action-button.secondary {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
	}

	.action-button.secondary:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.filters {
		display: flex;
		gap: var(--space-4);
		margin-bottom: var(--space-4);
		flex-wrap: wrap;
		align-items: center;
	}

	.search-box {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1;
		max-width: 400px;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-muted);
	}

	.search-box:focus-within {
		border-color: var(--color-accent);
	}

	.search-box input {
		flex: 1;
		border: none;
		background: none;
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.search-box input:focus {
		outline: none;
	}

	.category-filter-row {
		margin-bottom: var(--space-4);
	}

	.loading-state,
	.error-state,
	.empty-state {
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

	.empty-state h2 {
		margin: 0;
		font-size: var(--font-size-lg);
		color: var(--color-text-secondary);
	}

	.empty-state p {
		margin: 0;
		max-width: 400px;
	}

	.empty-actions {
		display: flex;
		gap: var(--space-3);
		margin-top: var(--space-4);
		flex-wrap: wrap;
		justify-content: center;
	}

	.action-cta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-5);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
	}

	.action-cta.primary {
		background: var(--color-accent);
		border: none;
		color: white;
	}

	.action-cta.primary:hover {
		opacity: 0.9;
	}

	.action-cta.secondary {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
	}

	.action-cta.secondary:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.sources-summary {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.clear-filters {
		padding: var(--space-1) var(--space-2);
		background: none;
		border: none;
		color: var(--color-accent);
		font-size: var(--font-size-sm);
		cursor: pointer;
	}

	.clear-filters:hover {
		text-decoration: underline;
	}

	.source-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
</style>
