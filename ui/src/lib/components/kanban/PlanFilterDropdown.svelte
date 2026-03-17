<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plans: PlanWithStatus[];
		selectedSlug: string | null;
		onSelect: (slug: string | null) => void;
	}

	let { plans, selectedSlug, onSelect }: Props = $props();

	let open = $state(false);

	const selectedLabel = $derived(
		selectedSlug ? plans.find((p) => p.slug === selectedSlug)?.slug ?? 'All Plans' : 'All Plans'
	);

	function select(slug: string | null) {
		onSelect(slug);
		open = false;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') open = false;
	}
</script>

<svelte:window onkeydown={open ? handleKeydown : undefined} />

<div class="plan-filter">
	<button class="filter-trigger" onclick={() => (open = !open)} aria-expanded={open}>
		<Icon name="filter" size={14} />
		<span>{selectedLabel}</span>
		<Icon name="chevron-down" size={12} />
	</button>

	{#if open}
		<button
			class="backdrop"
			onclick={() => (open = false)}
			tabindex="-1"
			aria-hidden="true"
		></button>
		<div class="dropdown" role="menu" aria-label="Filter by plan">
			<button
				class="dropdown-item"
				class:selected={selectedSlug === null}
				role="menuitem"
				onclick={() => select(null)}
			>
				All Plans
			</button>
			{#each plans as plan (plan.slug)}
				<button
					class="dropdown-item"
					class:selected={selectedSlug === plan.slug}
					role="menuitem"
					onclick={() => select(plan.slug)}
				>
					{plan.slug}
				</button>
			{/each}
		</div>
	{/if}
</div>

<style>
	.plan-filter {
		position: relative;
	}

	.filter-trigger {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.filter-trigger:hover {
		border-color: var(--color-border-focus);
		color: var(--color-text-primary);
	}

	.backdrop {
		position: fixed;
		inset: 0;
		z-index: var(--z-dropdown);
	}

	.dropdown {
		position: absolute;
		top: calc(100% + var(--space-1));
		left: 0;
		min-width: 180px;
		background: var(--color-bg-elevated);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		box-shadow: var(--shadow-md);
		z-index: calc(var(--z-dropdown) + 1);
		overflow-y: auto;
		max-height: 240px;
	}

	.dropdown-item {
		display: block;
		width: 100%;
		padding: var(--space-2) var(--space-3);
		border: none;
		background: none;
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		text-align: left;
		cursor: pointer;
		transition: background var(--transition-fast);
	}

	.dropdown-item:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.dropdown-item.selected {
		color: var(--color-accent);
		font-weight: var(--font-weight-medium);
	}
</style>
