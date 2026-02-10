<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import EntityCard from '$lib/components/entities/EntityCard.svelte';
	import { api } from '$lib/api/client';
	import type { Entity, EntityType } from '$lib/types';

	let entities = $state<Entity[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let searchQuery = $state('');
	let selectedType = $state<EntityType | ''>('');
	let entityCounts = $state<Record<string, number>>({});

	const entityTypes: { value: EntityType | ''; label: string }[] = [
		{ value: '', label: 'All Types' },
		{ value: 'code', label: 'Code' },
		{ value: 'proposal', label: 'Proposals' },
		{ value: 'spec', label: 'Specifications' },
		{ value: 'task', label: 'Tasks' },
		{ value: 'loop', label: 'Loops' },
		{ value: 'activity', label: 'Activities' }
	];

	async function loadEntities() {
		loading = true;
		error = null;
		try {
			const params: Record<string, unknown> = {};
			if (selectedType) params.type = selectedType;
			if (searchQuery) params.query = searchQuery;

			entities = await api.entities.list(params);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load entities';
			entities = [];
		} finally {
			loading = false;
		}
	}

	async function loadCounts() {
		try {
			const result = await api.entities.count();
			entityCounts = result.byType;
		} catch {
			// Silently fail
		}
	}

	function handleEntityClick(entity: Entity) {
		goto(`/entities/${encodeURIComponent(entity.id)}`);
	}

	function handleSearch(e: Event) {
		const target = e.target as HTMLInputElement;
		searchQuery = target.value;
	}

	function handleTypeChange(e: Event) {
		const target = e.target as HTMLSelectElement;
		selectedType = target.value as EntityType | '';
	}

	// Debounced search
	let searchTimeout: ReturnType<typeof setTimeout>;
	$effect(() => {
		// Subscribe to searchQuery changes
		const _ = searchQuery;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(loadEntities, 300);
	});

	// Reload when type changes
	$effect(() => {
		// Subscribe to selectedType changes
		const _ = selectedType;
		loadEntities();
	});

	// Only load counts on mount - loadEntities is triggered by the selectedType effect
	onMount(() => {
		loadCounts();
	});
</script>

<svelte:head>
	<title>Entity Browser - Semspec</title>
</svelte:head>

<div class="entities-page">
	<header class="page-header">
		<h1>Entity Browser</h1>
		<p class="subtitle">Explore the knowledge graph</p>
	</header>

	<div class="filters">
		<div class="search-box">
			<Icon name="search" size={18} />
			<input
				type="search"
				placeholder="Search entities..."
				value={searchQuery}
				oninput={handleSearch}
				aria-label="Search entities"
			/>
		</div>

		<select
			value={selectedType}
			onchange={handleTypeChange}
			aria-label="Filter by type"
		>
			{#each entityTypes as type}
				<option value={type.value}>
					{type.label}
					{#if type.value && entityCounts[type.value]}
						({entityCounts[type.value]})
					{/if}
				</option>
			{/each}
		</select>
	</div>

	{#if loading}
		<div class="loading-state">
			<Icon name="loader" size={24} />
			<span>Loading entities...</span>
		</div>
	{:else if error}
		<div class="error-state">
			<Icon name="alert-circle" size={24} />
			<span>{error}</span>
			<button onclick={loadEntities}>Retry</button>
		</div>
	{:else if entities.length === 0}
		<div class="empty-state">
			<Icon name="database" size={48} />
			<h2>No entities found</h2>
			<p>
				{#if searchQuery || selectedType}
					Try adjusting your search or filters
				{:else}
					The knowledge graph is empty. Run AST indexing or create proposals to populate it.
				{/if}
			</p>
		</div>
	{:else}
		<div class="entity-list">
			{#each entities as entity (entity.id)}
				<EntityCard {entity} onclick={() => handleEntityClick(entity)} />
			{/each}
		</div>
	{/if}
</div>

<style>
	.entities-page {
		max-width: 1200px;
		margin: 0 auto;
		padding: var(--space-6);
	}

	.page-header {
		margin-bottom: var(--space-6);
	}

	.page-header h1 {
		margin: 0;
		font-size: var(--font-size-2xl);
		color: var(--color-text-primary);
	}

	.subtitle {
		margin: var(--space-1) 0 0;
		color: var(--color-text-muted);
	}

	.filters {
		display: flex;
		gap: var(--space-4);
		margin-bottom: var(--space-6);
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

	select {
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
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

	.entity-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
</style>
