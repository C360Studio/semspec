<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { attentionStore } from '$lib/stores/attention.svelte';

	let collapsed = $state(false);

	const count = $derived(attentionStore.count);
	const items = $derived(attentionStore.items.slice(0, 3)); // Show first 3

	function getIcon(type: string): string {
		switch (type) {
			case 'approval_needed':
				return 'check-circle';
			case 'question_pending':
				return 'help-circle';
			case 'task_failed':
				return 'alert-circle';
			case 'task_blocked':
				return 'pause';
			default:
				return 'alert-triangle';
		}
	}
</script>

{#if count > 0}
	<div class="attention-banner" class:collapsed>
		<button
			class="toggle"
			onclick={() => (collapsed = !collapsed)}
			aria-expanded={!collapsed}
			aria-controls="attention-items"
			aria-label="Toggle attention items"
		>
			<Icon name="alert-triangle" size={16} />
			<span class="count">{count} {count === 1 ? 'item needs' : 'items need'} your attention</span>
			<Icon name={collapsed ? 'chevron-down' : 'chevron-up'} size={16} />
		</button>

		{#if !collapsed}
			<div class="items" id="attention-items" role="region" aria-label="Attention items">
				{#each items as item}
					<a href={item.action_url} class="item">
						<Icon name={getIcon(item.type)} size={14} />
						<span class="title">{item.title}</span>
					</a>
				{/each}
				{#if count > 3}
					<a href="/activity" class="view-all">View all {count} items</a>
				{/if}
			</div>
		{/if}
	</div>
{/if}

<style>
	.attention-banner {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		border: 1px solid var(--color-warning);
		border-radius: var(--radius-lg);
		margin-bottom: var(--space-4);
		overflow: hidden;
	}

	.toggle {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-3) var(--space-4);
		background: transparent;
		border: none;
		color: var(--color-warning);
		cursor: pointer;
		text-align: left;
	}

	.toggle:hover {
		background: rgba(245, 158, 11, 0.05);
	}

	.count {
		flex: 1;
		font-weight: var(--font-weight-medium);
	}

	.items {
		border-top: 1px solid var(--color-warning);
		padding: var(--space-2) var(--space-4);
	}

	.item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		color: var(--color-text-primary);
		text-decoration: none;
		font-size: var(--font-size-sm);
		transition: color var(--transition-fast);
	}

	.item:hover {
		color: var(--color-accent);
		text-decoration: none;
	}

	.item + .item {
		border-top: 1px solid var(--color-border);
	}

	.title {
		flex: 1;
	}

	.view-all {
		display: block;
		padding: var(--space-2) 0;
		color: var(--color-accent);
		text-decoration: none;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border-top: 1px solid var(--color-border);
	}

	.view-all:hover {
		text-decoration: underline;
	}

	.collapsed .toggle {
		border-radius: var(--radius-lg);
	}
</style>
