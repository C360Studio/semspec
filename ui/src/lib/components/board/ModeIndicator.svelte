<script lang="ts">
	interface Props {
		approved: boolean;
		compact?: boolean;
	}

	let { approved, compact = false }: Props = $props();
</script>

<div
	class="mode-indicator"
	class:approved
	class:draft={!approved}
	class:compact
	role="status"
	aria-label={approved ? 'Approved plan' : 'Draft plan'}
>
	<span class="icon" aria-hidden="true">
		{#if approved}
			&#x2713;
		{:else}
			&#x25D0;
		{/if}
	</span>
	{#if !compact}
		<span class="label">{approved ? 'Approved' : 'Draft'}</span>
	{/if}
</div>

<style>
	.mode-indicator {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		transition: all var(--transition-fast);
	}

	.mode-indicator.compact {
		padding: 2px 6px;
	}

	.mode-indicator.approved {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border: 1px solid var(--color-accent);
	}

	.mode-indicator.draft {
		background: transparent;
		color: var(--color-text-muted);
		border: 1px dashed var(--color-border);
	}

	.icon {
		font-size: var(--font-size-sm);
	}

	.draft .icon {
		animation: slow-spin 4s linear infinite;
	}

	.label {
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	@keyframes slow-spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
