<script lang="ts">
	/**
	 * Knowledge Graph Explorer
	 *
	 * Visualizes the semspec knowledge graph using ThreePanelLayout:
	 * - Left panel:   (closed by default — not used)
	 * - Center panel: GraphFilters toolbar (2 rows) + SigmaCanvas (fills remaining height)
	 * - Right panel:  GraphDetail for the selected entity
	 *
	 * Data flow:
	 * - On mount, calls graphStore.loadInitialGraph via a graphApiAdapter that
	 *   bridges graphApi + graphTransform to the GraphStoreAdapter interface.
	 * - Selection, hover, and expand events update graphStore state directly.
	 * - Filtered entities/relationships from graphStore feed SigmaCanvas.
	 * - NLQ search is handled via graphStore.searchEntities (calls graphApi.globalSearch).
	 */

	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import SigmaCanvas from '$lib/components/graph/SigmaCanvas.svelte';
	import GraphFilters from '$lib/components/graph/GraphFilters.svelte';
	import GraphDetail from '$lib/components/graph/GraphDetail.svelte';

	import { graphStore } from '$lib/stores/graphStore.svelte';
	import type { GraphStoreAdapter } from '$lib/stores/graphStore.svelte';
	import { graphApi } from '$lib/services/graphApi';
	import { transformPathSearchResult, transformGlobalSearchResult } from '$lib/services/graphTransform';
	import type { ClassificationMeta } from '$lib/api/graph-types';

	// ---------------------------------------------------------------------------
	// GraphStoreAdapter
	//
	// graphStore expects listEntities / getEntityNeighbors / searchEntities.
	// graphApi exposes getEntitiesByPrefix / pathSearch / globalSearch.
	// This adapter bridges the two without modifying either file.
	// ---------------------------------------------------------------------------
	const graphApiAdapter: GraphStoreAdapter = {
		async listEntities({ prefix = '', limit = 200 }) {
			const backendEntities = await graphApi.getEntitiesByPrefix(prefix, limit);
			const entities = transformPathSearchResult({ entities: backendEntities, edges: [] });
			return { entities };
		},

		async getEntityNeighbors(entityId: string) {
			const result = await graphApi.pathSearch(entityId, 2, 50);
			const entities = transformPathSearchResult(result);
			return { entities };
		},

		async searchEntities({ query, limit = 100 }) {
			const result = await graphApi.globalSearch(query);
			// Use transformGlobalSearchResult to preserve explicit relationships from NLQ
			const allEntities = transformGlobalSearchResult(result);
			// Store classification for display in the toolbar
			lastClassification = result.classification ?? null;
			return { entities: allEntities.slice(0, limit) };
		}
	};

	// ---------------------------------------------------------------------------
	// Load initial graph on first mount only.
	// If the user navigates away and back, the existing store data is preserved
	// rather than silently resetting their exploration. The refresh button
	// provides an explicit way to reload.
	// ---------------------------------------------------------------------------
	$effect(() => {
		if (graphStore.entities.size > 0) return;
		graphStore.loadInitialGraph(graphApiAdapter);
	});

	// ---------------------------------------------------------------------------
	// NLQ classification state
	// Passed to GraphFilters to show the badge after NLQ search.
	// ---------------------------------------------------------------------------
	let lastClassification = $state<ClassificationMeta | null>(null);
	let nlqSearching = $state(false);

	// ---------------------------------------------------------------------------
	// Derived passthrough values from the store
	// ---------------------------------------------------------------------------
	const filteredEntities = $derived(graphStore.filteredEntities);
	const filteredRelationships = $derived(graphStore.filteredRelationships);
	const selectedEntity = $derived(graphStore.selectedEntity);

	// ---------------------------------------------------------------------------
	// Event handlers — delegate straight to graphStore mutations
	// ---------------------------------------------------------------------------
	function handleEntitySelect(entityId: string | null) {
		graphStore.selectEntity(entityId);
		// Auto-open the right panel when something is selected
		if (entityId && !rightPanelOpen) {
			rightPanelOpen = true;
		}
	}

	function handleEntityHover(entityId: string | null) {
		graphStore.setHoveredEntity(entityId);
	}

	async function handleEntityExpand(entityId: string) {
		await graphStore.expandEntity(graphApiAdapter, entityId);
	}

	async function handleRefresh() {
		lastClassification = null;
		graphStore.clearEntities();
		await graphStore.loadInitialGraph(graphApiAdapter);
	}

	function handleToggleType(type: string) {
		graphStore.toggleEntityType(type);
	}

	function handleSearchChange(search: string) {
		graphStore.setFilters({ search });
	}

	async function handleNlqSearch(query: string) {
		nlqSearching = true;
		lastClassification = null;
		try {
			await graphStore.searchEntities(graphApiAdapter, query);
		} finally {
			nlqSearching = false;
		}
	}

	// Navigate to a related entity from the detail panel — select it and
	// expand it so its neighbors are visible in the graph.
	function handleRelatedEntitySelect(entityId: string) {
		graphStore.selectEntity(entityId);
		void graphStore.expandEntity(graphApiAdapter, entityId);
	}

	// ---------------------------------------------------------------------------
	// Panel state
	// ---------------------------------------------------------------------------
	let leftPanelOpen = $state(false);
	let rightPanelOpen = $state(true);
	let rightPanelWidth = $state(320);
</script>

<svelte:head>
	<title>Knowledge Graph - Semspec</title>
</svelte:head>

<ThreePanelLayout
	id="entities-graph"
	leftOpen={leftPanelOpen}
	rightOpen={rightPanelOpen}
	leftWidth={240}
	rightWidth={rightPanelWidth}
	onLeftToggle={(open) => (leftPanelOpen = open)}
	onRightToggle={(open) => (rightPanelOpen = open)}
>
	{#snippet leftPanel()}
		<!-- Left panel intentionally empty; closed by default -->
		<div></div>
	{/snippet}

	{#snippet centerPanel()}
		<div class="graph-page" data-testid="graph-page">
			<!-- Top toolbar: 2-row GraphFilters -->
			<GraphFilters
				visibleTypes={graphStore.visibleTypes}
				presentTypes={graphStore.presentEntityTypes}
				search={graphStore.filters.search}
				visibleCount={filteredEntities.length}
				totalCount={graphStore.entities.size}
				classification={lastClassification}
				searching={nlqSearching}
				onToggleType={handleToggleType}
				onSearchChange={handleSearchChange}
				onNlqSearch={handleNlqSearch}
				onShowAll={() => graphStore.showAllTypes()}
				onHideAll={() => graphStore.hideAllTypes()}
			/>

			{#if graphStore.error}
				<div class="error-banner" role="alert" data-testid="graph-error">
					<span class="error-icon" aria-hidden="true">!</span>
					{graphStore.error}
					<button
						class="error-dismiss"
						onclick={() => graphStore.setError(null)}
						aria-label="Dismiss error"
					>
						×
					</button>
				</div>
			{/if}

			<!-- Graph canvas — fills remaining height -->
			<div class="canvas-wrapper">
				<SigmaCanvas
					entities={filteredEntities}
					relationships={filteredRelationships}
					selectedEntityId={graphStore.selectedEntityId}
					hoveredEntityId={graphStore.hoveredEntityId}
					onEntitySelect={handleEntitySelect}
					onEntityHover={handleEntityHover}
					onEntityExpand={handleEntityExpand}
					onRefresh={handleRefresh}
					loading={graphStore.loading}
				/>
			</div>
		</div>
	{/snippet}

	{#snippet rightPanel()}
		<GraphDetail
			entity={selectedEntity ?? null}
			onEntitySelect={handleRelatedEntitySelect}
		/>
	{/snippet}
</ThreePanelLayout>

<style>
	.graph-page {
		height: 100%;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	/* Canvas fills all remaining height below the toolbar */
	.canvas-wrapper {
		flex: 1;
		min-height: 0;
		position: relative;
	}

	/* Error banner */
	.error-banner {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
		background: color-mix(in srgb, var(--color-error, #f87171) 15%, var(--color-bg-primary));
		color: var(--color-error, #f87171);
		font-size: 12px;
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.error-icon {
		font-weight: 700;
		flex-shrink: 0;
	}

	.error-dismiss {
		margin-left: auto;
		background: transparent;
		border: none;
		color: inherit;
		font-size: 16px;
		cursor: pointer;
		padding: 0 4px;
		opacity: 0.7;
		line-height: 1;
	}

	.error-dismiss:hover {
		opacity: 1;
	}
</style>
