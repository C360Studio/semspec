<script lang="ts">
	/**
	 * GraphDetail - Entity detail panel for the graph visualization
	 *
	 * Shows a complete breakdown of the selected semspec entity:
	 * - Color-coded type badge with instance label
	 * - Full entity ID with type and instance parsed from it, plus copy button
	 * - Properties table with confidence opacity
	 * - Outgoing and incoming relationships as clickable navigation links
	 * - Last-updated timestamp derived from the most recent property
	 */

	import type { GraphEntity } from '$lib/api/graph-types';
	import { getEntityLabel, getEntityTypeLabel, parseEntityId } from '$lib/api/graph-types';
	import { getEntityColor, getPredicateColor, getConfidenceOpacity } from '$lib/utils/entity-colors';

	interface GraphDetailProps {
		entity: GraphEntity | null;
		onEntitySelect: (id: string) => void;
	}

	let { entity, onEntitySelect }: GraphDetailProps = $props();

	// Derived display values
	const label = $derived(entity ? getEntityLabel(entity) : '');
	const typeLabel = $derived(entity ? getEntityTypeLabel(entity) : '');
	const entityColor = $derived(entity ? getEntityColor(entity.idParts) : '#888');

	function formatTimestamp(ts: number): string {
		return new Date(ts).toLocaleString();
	}

	function formatConfidence(confidence: number): string {
		return `${(confidence * 100).toFixed(0)}%`;
	}

	/** Show only the last segment of a dotted predicate name. */
	function shortPredicate(predicate: string): string {
		const parts = predicate.split('.');
		return parts[parts.length - 1] || predicate;
	}

	/** Show last segment of a dotted entity ID for display in relationship rows. */
	function shortEntityId(id: string): string {
		return parseEntityId(id).instance || id;
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text).catch(() => {
			// Silently fail — clipboard API may be unavailable in some contexts
		});
	}

	/** Navigate to a related entity when the user clicks a relationship row. */
	function handleRelatedEntityClick(entityId: string) {
		onEntitySelect(entityId);
	}
</script>

{#if entity}
	<div class="detail-panel" data-testid="graph-detail-panel">
		<!-- Header -->
		<div class="panel-header">
			<div class="entity-badge" style="background-color: {entityColor}" aria-hidden="true">
				{entity.idParts.type.charAt(0).toUpperCase()}
			</div>
			<div class="entity-title">
				<h3 class="entity-label" title={entity.id}>{label}</h3>
				<span class="entity-type">{typeLabel}</span>
			</div>
			<button
				class="copy-btn"
				onclick={() => copyToClipboard(entity!.id)}
				aria-label="Copy entity ID"
				title="Copy entity ID"
			>
				&#x2398;
			</button>
		</div>

		<!-- ID Breakdown -->
		<section class="section" aria-label="Entity ID breakdown">
			<h4 class="section-title">Entity ID</h4>
			<code class="entity-id-full">{entity.id}</code>
			<div class="id-breakdown">
				<div class="id-part">
					<span class="id-label">type</span>
					<span class="id-value">{entity.idParts.type}</span>
				</div>
				{#if entity.idParts.prefix}
					<div class="id-part">
						<span class="id-label">path</span>
						<span class="id-value">{entity.idParts.prefix}</span>
					</div>
				{/if}
				<div class="id-part">
					<span class="id-label">instance</span>
					<span class="id-value">{entity.idParts.instance}</span>
				</div>
			</div>
		</section>

		<!-- Properties -->
		{#if entity.properties.length > 0}
			<section class="section" aria-label="Properties">
				<h4 class="section-title">Properties ({entity.properties.length})</h4>
				<div class="properties-list">
					{#each entity.properties as prop, idx (prop.predicate + idx)}
						<div class="property-row">
							<span
								class="property-predicate"
								style="color: {getPredicateColor(prop.predicate)}"
								title={prop.predicate}
							>
								{shortPredicate(prop.predicate)}
							</span>
							<span class="property-value" title={String(prop.object)}>
								{String(prop.object)}
							</span>
							<span
								class="property-confidence"
								style="opacity: {getConfidenceOpacity(prop.confidence)}"
								title="Confidence: {formatConfidence(prop.confidence)}"
							>
								{formatConfidence(prop.confidence)}
							</span>
						</div>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Outgoing Relationships -->
		{#if entity.outgoing.length > 0}
			<section class="section" aria-label="Outgoing relationships">
				<h4 class="section-title">Outgoing ({entity.outgoing.length})</h4>
				<div class="relationships-list">
					{#each entity.outgoing as rel (rel.id)}
						<button
							class="relationship-row"
							onclick={() => handleRelatedEntityClick(rel.targetId)}
							title="Navigate to {rel.targetId}"
						>
							<span class="rel-predicate" style="color: {getPredicateColor(rel.predicate)}">
								{shortPredicate(rel.predicate)}
							</span>
							<span class="rel-arrow" aria-hidden="true">→</span>
							<span class="rel-target">{shortEntityId(rel.targetId)}</span>
							<span
								class="rel-confidence"
								style="opacity: {getConfidenceOpacity(rel.confidence)}"
								aria-label="Confidence {formatConfidence(rel.confidence)}"
							>
								{formatConfidence(rel.confidence)}
							</span>
						</button>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Incoming Relationships -->
		{#if entity.incoming.length > 0}
			<section class="section" aria-label="Incoming relationships">
				<h4 class="section-title">Incoming ({entity.incoming.length})</h4>
				<div class="relationships-list">
					{#each entity.incoming as rel (rel.id)}
						<button
							class="relationship-row"
							onclick={() => handleRelatedEntityClick(rel.sourceId)}
							title="Navigate to {rel.sourceId}"
						>
							<span class="rel-source">{shortEntityId(rel.sourceId)}</span>
							<span class="rel-arrow" aria-hidden="true">←</span>
							<span class="rel-predicate" style="color: {getPredicateColor(rel.predicate)}">
								{shortPredicate(rel.predicate)}
							</span>
							<span
								class="rel-confidence"
								style="opacity: {getConfidenceOpacity(rel.confidence)}"
								aria-label="Confidence {formatConfidence(rel.confidence)}"
							>
								{formatConfidence(rel.confidence)}
							</span>
						</button>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Last updated timestamp -->
		{#if entity.properties.length > 0}
			{@const latestProp = entity.properties.reduce((latest, prop) =>
				prop.timestamp > latest.timestamp ? prop : latest
			)}
			<section class="section section-footer">
				<span class="timestamp">Last updated: {formatTimestamp(latestProp.timestamp)}</span>
			</section>
		{/if}
	</div>
{:else}
	<div class="detail-panel detail-panel-empty" data-testid="graph-detail-panel-empty">
		<p class="empty-message">Select an entity to view details</p>
	</div>
{/if}

<style>
	.detail-panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow-y: auto;
		background: var(--color-bg-secondary);
	}

	.detail-panel-empty {
		justify-content: center;
		align-items: center;
	}

	.empty-message {
		color: var(--color-text-muted);
		font-size: 13px;
		font-style: italic;
		text-align: center;
		padding: var(--space-6, 24px);
	}

	/* Header */
	.panel-header {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px;
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-primary);
		flex-shrink: 0;
	}

	.entity-badge {
		width: 36px;
		height: 36px;
		border-radius: 50%;
		display: flex;
		align-items: center;
		justify-content: center;
		color: white;
		font-weight: 600;
		font-size: 16px;
		flex-shrink: 0;
	}

	.entity-title {
		flex: 1;
		min-width: 0;
	}

	.entity-label {
		margin: 0;
		font-size: 14px;
		font-weight: 600;
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.entity-type {
		font-size: 11px;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.5px;
	}

	.copy-btn {
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		color: var(--color-text-muted);
		cursor: pointer;
		font-size: 16px;
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		transition: background-color 150ms ease, color 150ms ease;
	}

	.copy-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	/* Sections */
	.section {
		padding: 12px;
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.section-title {
		margin: 0 0 8px 0;
		font-size: 11px;
		font-weight: 600;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.5px;
	}

	.section-footer {
		border-bottom: none;
	}

	/* ID Breakdown */
	.entity-id-full {
		font-family: var(--font-mono, monospace);
		font-size: 10px;
		color: var(--color-text-secondary);
		word-break: break-all;
		display: block;
		margin-bottom: 8px;
	}

	.id-breakdown {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.id-part {
		display: flex;
		gap: 8px;
		align-items: baseline;
	}

	.id-label {
		font-size: 9px;
		color: var(--color-text-muted);
		text-transform: uppercase;
		min-width: 48px;
		flex-shrink: 0;
	}

	.id-value {
		font-size: 12px;
		color: var(--color-text-primary);
		font-family: var(--font-mono, monospace);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	/* Properties */
	.properties-list {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.property-row {
		display: grid;
		grid-template-columns: 1fr 1fr auto;
		gap: 8px;
		align-items: center;
		padding: 4px 6px;
		background: var(--color-bg-primary);
		border-radius: var(--radius-sm, 4px);
		font-size: 11px;
	}

	.property-predicate {
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.property-value {
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		font-family: var(--font-mono, monospace);
	}

	.property-confidence {
		font-size: 10px;
		color: var(--color-text-muted);
		min-width: 32px;
		text-align: right;
	}

	/* Relationships */
	.relationships-list {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.relationship-row {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 8px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		font-size: 11px;
		cursor: pointer;
		transition: border-color 150ms ease, background-color 150ms ease;
		text-align: left;
		width: 100%;
		color: var(--color-text-primary);
	}

	.relationship-row:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
	}

	.rel-predicate {
		font-weight: 500;
		white-space: nowrap;
	}

	.rel-arrow {
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.rel-source,
	.rel-target {
		color: var(--color-text-primary);
		font-family: var(--font-mono, monospace);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		flex: 1;
		min-width: 0;
	}

	.rel-confidence {
		font-size: 10px;
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	/* Footer */
	.timestamp {
		font-size: 10px;
		color: var(--color-text-muted);
	}
</style>
