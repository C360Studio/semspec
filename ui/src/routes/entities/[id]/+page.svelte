<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import RelationshipList from '$lib/components/entities/RelationshipList.svelte';
	import { api } from '$lib/api/client';
	import type { EntityWithRelationships, Relationship, EntityType } from '$lib/types';

	let entity = $state<EntityWithRelationships | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// BFO/CCO classification mapping
	const bfoClassifications: Record<EntityType, { bfo: string; cco: string }> = {
		code: { bfo: 'GenericallyDependentContinuant', cco: 'SoftwareCode' },
		proposal: { bfo: 'GenericallyDependentContinuant', cco: 'InformationContentEntity' },
		spec: { bfo: 'GenericallyDependentContinuant', cco: 'DirectiveICE' },
		task: { bfo: 'GenericallyDependentContinuant', cco: 'PlanSpecification' },
		loop: { bfo: 'Process', cco: 'ActOfArtifactProcessing' },
		activity: { bfo: 'Process', cco: 'Act' }
	};

	// PROV-O relationship predicates
	const provPredicates = [
		'prov.generation.activity',
		'prov.attribution.agent',
		'prov.derivation.source',
		'prov.usage.entity',
		'prov.association.agent'
	];

	async function loadEntity(id: string) {
		loading = true;
		error = null;
		try {
			entity = await api.entities.get(id);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load entity';
			entity = null;
		} finally {
			loading = false;
		}
	}

	function getClassification(type: EntityType) {
		return bfoClassifications[type] || { bfo: 'Entity', cco: 'Entity' };
	}

	function isProvRelationship(predicate: string): boolean {
		return provPredicates.some(p => predicate.startsWith(p.split('.')[0]));
	}

	function getRelationshipsByCategory(relationships: Relationship[]) {
		const categories = {
			provenance: [] as Relationship[],
			structure: [] as Relationship[],
			semantic: [] as Relationship[]
		};

		for (const rel of relationships) {
			if (isProvRelationship(rel.predicate)) {
				categories.provenance.push(rel);
			} else if (rel.predicate.includes('structure') || rel.predicate.includes('contains') || rel.predicate.includes('belongs')) {
				categories.structure.push(rel);
			} else {
				categories.semantic.push(rel);
			}
		}

		return categories;
	}

	function handleRelationshipClick(rel: Relationship) {
		goto(`/entities/${encodeURIComponent(rel.targetId)}`);
	}

	function goBack() {
		goto('/entities');
	}

	// Watch for id changes ($effect handles both initial load AND param changes)
	$effect(() => {
		const id = $page.params.id;
		if (id) {
			loadEntity(decodeURIComponent(id));
		}
	});
</script>

<svelte:head>
	<title>{entity?.name || 'Entity'} - Semspec</title>
</svelte:head>

<div class="entity-detail-page">
	<button class="back-button" onclick={goBack}>
		<Icon name="arrow-left" size={18} />
		<span>Back to Entities</span>
	</button>

	{#if loading}
		<div class="loading-state">
			<Icon name="loader" size={24} />
			<span>Loading entity...</span>
		</div>
	{:else if error}
		<div class="error-state">
			<Icon name="alert-circle" size={24} />
			<span>{error}</span>
			<button onclick={() => $page.params.id && loadEntity($page.params.id)}>Retry</button>
		</div>
	{:else if entity}
		<header class="entity-header">
			<div class="entity-title">
				<h1>{entity.name}</h1>
				<span class="entity-type">{entity.type}</span>
			</div>
			<p class="entity-id">{entity.id}</p>
		</header>

		<div class="classification-badges">
			<span class="badge bfo-badge" title="BFO Classification">
				BFO: {getClassification(entity.type).bfo}
			</span>
			<span class="badge cco-badge" title="CCO Classification">
				CCO: {getClassification(entity.type).cco}
			</span>
		</div>

		<section class="predicates-section">
			<h2>Predicates</h2>
			<div class="predicates-list">
				{#each Object.entries(entity.predicates) as [key, value]}
					<div class="predicate-row">
						<span class="predicate-key">{key}</span>
						<span class="predicate-value">{JSON.stringify(value)}</span>
					</div>
				{/each}
			</div>
		</section>

		{#if entity.relationships && entity.relationships.length > 0}
			{@const categories = getRelationshipsByCategory(entity.relationships)}

			<RelationshipList
				relationships={categories.provenance}
				title="Provenance"
				icon="git-branch"
				onRelationshipClick={handleRelationshipClick}
			/>

			<RelationshipList
				relationships={categories.structure}
				title="Structure"
				icon="folder-tree"
				onRelationshipClick={handleRelationshipClick}
			/>

			<RelationshipList
				relationships={categories.semantic}
				title="Semantic"
				icon="link"
				onRelationshipClick={handleRelationshipClick}
			/>
		{/if}

		{#if entity.createdAt || entity.updatedAt}
			<section class="timestamps-section">
				<h2>Timestamps</h2>
				<div class="timestamps">
					{#if entity.createdAt}
						<div class="timestamp">
							<span class="label">Created:</span>
							<span class="value">{new Date(entity.createdAt).toLocaleString()}</span>
						</div>
					{/if}
					{#if entity.updatedAt}
						<div class="timestamp">
							<span class="label">Updated:</span>
							<span class="value">{new Date(entity.updatedAt).toLocaleString()}</span>
						</div>
					{/if}
				</div>
			</section>
		{/if}
	{/if}
</div>

<style>
	.entity-detail-page {
		max-width: 900px;
		margin: 0 auto;
		padding: var(--space-6);
	}

	.back-button {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		margin-bottom: var(--space-6);
		background: none;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.back-button:hover {
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
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

	.entity-header {
		margin-bottom: var(--space-4);
	}

	.entity-title {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.entity-title h1 {
		margin: 0;
		font-size: var(--font-size-2xl);
		color: var(--color-text-primary);
	}

	.entity-type {
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		text-transform: uppercase;
	}

	.entity-id {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		word-break: break-all;
	}

	.classification-badges {
		display: flex;
		gap: var(--space-3);
		margin-bottom: var(--space-6);
	}

	.badge {
		padding: var(--space-1) var(--space-3);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
	}

	.bfo-badge {
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.cco-badge {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	section {
		margin-bottom: var(--space-6);
	}

	section h2 {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0 0 var(--space-3);
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.predicates-list {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.predicate-row {
		display: flex;
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.predicate-row:last-child {
		border-bottom: none;
	}

	.predicate-key {
		flex: 0 0 40%;
		font-family: var(--font-mono);
		font-size: var(--font-size-sm);
		color: var(--color-accent);
	}

	.predicate-value {
		flex: 1;
		font-family: var(--font-mono);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		word-break: break-all;
	}

	.timestamps {
		display: flex;
		gap: var(--space-6);
	}

	.timestamp {
		display: flex;
		gap: var(--space-2);
	}

	.timestamp .label {
		color: var(--color-text-muted);
	}

	.timestamp .value {
		color: var(--color-text-primary);
	}
</style>
