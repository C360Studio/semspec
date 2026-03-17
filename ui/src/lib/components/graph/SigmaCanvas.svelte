<script lang="ts">
	/**
	 * SigmaCanvas - WebGL graph renderer using Sigma.js + graphology
	 *
	 * Initialises Sigma inside a $effect (SSR-safe — checks typeof window).
	 * Cleans up sigma + layout on destroy via $effect return value.
	 * Node highlighting: selected node + direct neighbors are full opacity;
	 * all others are faded to #525252. Edge dimming mirrors the same rule.
	 */

	import { MultiDirectedGraph } from 'graphology';
	import Sigma from 'sigma';
	import type { GraphEntity, GraphRelationship } from '$lib/api/graph-types';
	import { syncStoreToGraph } from '$lib/utils/graphology-adapter';
	import { LayoutController } from '$lib/utils/sigma-layout';

	interface SigmaCanvasProps {
		entities: GraphEntity[];
		relationships: GraphRelationship[];
		selectedEntityId?: string | null;
		hoveredEntityId?: string | null;
		onEntitySelect?: (entityId: string | null) => void;
		onEntityExpand?: (entityId: string) => void;
		onEntityHover?: (entityId: string | null) => void;
		onRefresh?: () => void;
		loading?: boolean;
	}

	let {
		entities,
		relationships,
		selectedEntityId = null,
		hoveredEntityId = null,
		onEntitySelect,
		onEntityExpand,
		onEntityHover,
		onRefresh,
		loading = false
	}: SigmaCanvasProps = $props();

	let containerElement: HTMLDivElement;
	let sigma: Sigma | null = null;
	let graph: MultiDirectedGraph | null = null;
	let layout: LayoutController | null = null;
	let lastEntityCount = 0;

	// Initialise Sigma once the container is in the DOM — SSR safe
	$effect(() => {
		// Guard: Sigma requires a real browser environment
		if (typeof window === 'undefined') return;
		if (!containerElement) return;

		graph = new MultiDirectedGraph();
		layout = new LayoutController();

		sigma = new Sigma(graph, containerElement, {
			allowInvalidContainer: true,
			renderEdgeLabels: false,
			defaultEdgeType: 'arrow',
			labelRenderedSizeThreshold: 8,
			labelColor: { color: '#f4f4f4' },
			defaultDrawNodeHover: (context, data, settings) => {
				const size = data.size || 5;
				const x = data.x;
				const y = data.y;
				const color = data.color || '#6b7280';

				// Draw a colored ring around the node instead of Sigma's default white halo
				context.beginPath();
				context.arc(x, y, size + 3, 0, Math.PI * 2);
				context.strokeStyle = color;
				context.lineWidth = 2;
				context.stroke();
				context.closePath();

				// Redraw the node circle
				context.beginPath();
				context.arc(x, y, size, 0, Math.PI * 2);
				context.fillStyle = color;
				context.fill();
				context.closePath();

				// Draw the label
				if (data.label) {
					const fontSize = settings.labelSize || 14;
					context.font = `${fontSize}px ${settings.labelFont || 'sans-serif'}`;
					context.fillStyle = '#f4f4f4';
					context.fillText(data.label, x + size + 4, y + fontSize / 3);
				}
			},
			nodeReducer: (node, data) => {
				const res = { ...data };

				if (selectedEntityId && node !== selectedEntityId) {
					const isNeighbor =
						graph!.hasEdge(selectedEntityId, node) ||
						graph!.hasEdge(node, selectedEntityId);
					if (!isNeighbor) {
						res.color = '#525252';
						res.label = '';
					}
				}

				if (node === hoveredEntityId) {
					res.highlighted = true;
				}

				return res;
			},
			edgeReducer: (edge, data) => {
				const res = { ...data };

				if (selectedEntityId) {
					const source = graph!.source(edge);
					const target = graph!.target(edge);
					const isConnected =
						source === selectedEntityId || target === selectedEntityId;
					if (!isConnected) {
						res.color = '#393939';
					}
				}

				return res;
			}
		});

		// Event handlers
		sigma.on('clickNode', ({ node }) => {
			onEntitySelect?.(node === selectedEntityId ? null : node);
		});

		sigma.on('doubleClickNode', ({ node, event }) => {
			event.original.preventDefault();
			onEntityExpand?.(node);
		});

		sigma.on('enterNode', ({ node }) => {
			onEntityHover?.(node);
		});

		sigma.on('leaveNode', () => {
			onEntityHover?.(null);
		});

		sigma.on('clickStage', () => {
			onEntitySelect?.(null);
		});

		// Cleanup on destroy
		return () => {
			layout?.stop();
			sigma?.kill();
			sigma = null;
			graph = null;
			layout = null;
		};
	});

	// Sync graphology when entity/relationship data changes
	$effect(() => {
		if (!graph || !sigma || !layout) return;
		// Touch reactive arrays to subscribe to changes
		void entities.length;
		void relationships.length;
		if (entities.length === 0 && graph.order === 0) return;

		const prevCount = lastEntityCount;
		syncStoreToGraph(graph, entities, relationships);
		// Only restart layout when new data arrives (load/expand), not on filter toggle
		if (entities.length !== prevCount) {
			layout.start(graph);
			lastEntityCount = entities.length;
		}
		sigma.refresh();
	});

	// Refresh renderer when selection or hover state changes
	$effect(() => {
		if (!sigma) return;
		// Touch reactive props to subscribe to their changes
		void selectedEntityId;
		void hoveredEntityId;
		sigma.refresh();
	});

	function handleZoomIn() {
		if (!sigma) return;
		sigma.getCamera().animatedZoom({ duration: 200 });
	}

	function handleZoomOut() {
		if (!sigma) return;
		sigma.getCamera().animatedUnzoom({ duration: 200 });
	}

	function handleFitToContent() {
		if (!sigma) return;
		sigma.getCamera().animatedReset({ duration: 300 });
	}

	function handleRefreshLayout() {
		if (!graph || !layout || !sigma) return;
		layout.start(graph);
		sigma.refresh();
	}
</script>

<div class="sigma-canvas-container" data-testid="sigma-canvas">
	<!-- Zoom + layout controls -->
	<div class="zoom-controls" role="toolbar" aria-label="Graph controls">
		<button onclick={handleZoomIn} aria-label="Zoom in" title="Zoom in">+</button>
		<button onclick={handleZoomOut} aria-label="Zoom out" title="Zoom out">−</button>
		<button onclick={handleFitToContent} aria-label="Fit to content" title="Fit to content">
			<span class="fit-icon">&#x2A01;</span>
		</button>
		<button onclick={handleRefreshLayout} aria-label="Refresh layout" title="Re-run force layout">
			<span class="layout-icon">&#x21C4;</span>
		</button>
		{#if onRefresh}
			<button
				onclick={onRefresh}
				aria-label="Refresh data"
				title="Reload graph data"
				disabled={loading}
				class:refreshing={loading}
			>
				<span class="refresh-icon">&#x21bb;</span>
			</button>
		{/if}
	</div>

	<!-- Sigma WebGL canvas -->
	<div
		class="sigma-container"
		bind:this={containerElement}
		role="img"
		aria-label="Knowledge graph visualization. Use the detail panel to navigate entities."
	></div>

	<!-- Stats overlay -->
	<div class="graph-stats" aria-live="polite">
		<span>{entities.length} entities</span>
		<span>{relationships.length} relationships</span>
	</div>

	<!-- Loading indicator -->
	{#if loading}
		<div class="loading-overlay" aria-label="Loading graph data">
			<span class="loading-spinner" aria-hidden="true">&#x21bb;</span>
			<span>Loading…</span>
		</div>
	{/if}

	<!-- Empty state -->
	{#if !loading && entities.length === 0}
		<div class="empty-state">
			<p>No entities to display.</p>
			<p class="empty-hint">Load initial data or adjust filters.</p>
		</div>
	{/if}
</div>

<style>
	.sigma-canvas-container {
		position: relative;
		width: 100%;
		height: 100%;
		overflow: hidden;
		background: var(--color-bg-primary);
	}

	.sigma-container {
		width: 100%;
		height: 100%;
	}

	/* Zoom / layout controls */
	.zoom-controls {
		position: absolute;
		top: 12px;
		right: 12px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		z-index: 10;
	}

	.zoom-controls button {
		width: 32px;
		height: 32px;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 6px);
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		font-size: 18px;
		font-weight: 500;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background-color 150ms ease, border-color 150ms ease;
	}

	.zoom-controls button:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
	}

	.zoom-controls button:active {
		transform: scale(0.95);
	}

	.zoom-controls button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.fit-icon,
	.layout-icon {
		font-size: 14px;
	}

	.refresh-icon {
		font-size: 16px;
	}

	.zoom-controls button.refreshing .refresh-icon {
		display: inline-block;
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	/* Stats overlay */
	.graph-stats {
		position: absolute;
		bottom: 12px;
		left: 12px;
		display: flex;
		gap: 12px;
		font-size: 11px;
		color: var(--color-text-muted);
		background: var(--color-bg-secondary);
		padding: 4px 8px;
		border-radius: var(--radius-sm, 4px);
		border: 1px solid var(--color-border);
	}

	/* Loading overlay */
	.loading-overlay {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 8px;
		background: color-mix(in srgb, var(--color-bg-primary) 80%, transparent);
		font-size: 13px;
		color: var(--color-text-muted);
		z-index: 20;
	}

	.loading-spinner {
		font-size: 28px;
		display: inline-block;
		animation: spin 1s linear infinite;
	}

	/* Empty state */
	.empty-state {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 4px;
		pointer-events: none;
	}

	.empty-state p {
		margin: 0;
		font-size: 14px;
		color: var(--color-text-muted);
	}

	.empty-hint {
		font-size: 12px;
		color: var(--color-text-muted);
		opacity: 0.7;
	}
</style>
