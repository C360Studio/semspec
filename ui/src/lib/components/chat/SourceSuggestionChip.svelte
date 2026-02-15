<script lang="ts">
	import { onMount } from 'svelte';
	import Icon from '$lib/components/shared/Icon.svelte';

	interface Props {
		type: 'url' | 'file';
		value: string;
		projectId: string;
		onAdd: () => Promise<void>;
		onDismiss: () => void;
	}

	let { type, value, projectId, onAdd, onDismiss }: Props = $props();

	let loading = $state(false);
	let addButtonRef = $state<HTMLButtonElement | null>(null);

	// Validate projectId reactively
	const isValid = $derived(Boolean(projectId));

	// Log warning if projectId is missing (only in dev)
	$effect(() => {
		if (!projectId) {
			console.warn('SourceSuggestionChip: projectId is required');
		}
	});

	// Truncate long values for display - use expression, not callback
	const displayValue = $derived(value.length > 50 ? value.slice(0, 47) + '...' : value);
	const icon = $derived(type === 'url' ? 'globe' : 'file');
	const actionLabel = $derived(type === 'url' ? 'Add as source' : 'Upload');

	// Focus the add button when chip appears for accessibility
	onMount(() => {
		addButtonRef?.focus();
	});

	async function handleAdd(): Promise<void> {
		if (!projectId) {
			console.error('SourceSuggestionChip: Cannot add source without projectId');
			return;
		}
		loading = true;
		try {
			await onAdd();
		} finally {
			loading = false;
		}
	}

	function handleKeydown(e: KeyboardEvent): void {
		if (e.key === 'Escape') {
			e.preventDefault();
			onDismiss();
		}
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
	class="chip"
	role="group"
	aria-label="Source suggestion: {type === 'url' ? 'URL' : 'file'} detected"
	onkeydown={handleKeydown}
>
	<div class="chip-content">
		<Icon name={icon} size={14} />
		<span class="value" title={value}>{displayValue}</span>
	</div>
	<div class="chip-actions">
		<button
			bind:this={addButtonRef}
			class="action-button primary"
			onclick={handleAdd}
			disabled={loading || !isValid}
			aria-label={actionLabel}
		>
			{#if loading}
				<Icon name="loader" size={14} />
			{:else}
				{actionLabel}
			{/if}
		</button>
		<button
			class="action-button dismiss"
			onclick={onDismiss}
			disabled={loading}
			aria-label="Dismiss suggestion"
		>
			<Icon name="x" size={14} />
		</button>
	</div>
</div>

<style>
	.chip {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		margin-bottom: var(--space-2);
	}

	.chip-content {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
		color: var(--color-text-secondary);
	}

	.value {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-family: var(--font-mono);
		font-size: var(--font-size-xs);
	}

	.chip-actions {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex-shrink: 0;
	}

	.action-button {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-1) var(--space-2);
		border: none;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: 500;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.action-button:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.action-button.primary {
		background: var(--color-accent);
		color: white;
	}

	.action-button.primary:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.action-button.dismiss {
		background: transparent;
		color: var(--color-text-muted);
		padding: var(--space-1);
	}

	.action-button.dismiss:hover:not(:disabled) {
		color: var(--color-text-secondary);
		background: var(--color-bg-secondary);
	}

	.action-button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* Loader animation */
	.action-button :global(svg.lucide-loader-2) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
