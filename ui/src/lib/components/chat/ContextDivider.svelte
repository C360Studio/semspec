<script lang="ts">
	/**
	 * ContextDivider - Visual separator showing context changes in chat.
	 *
	 * Displays when the chat context switches between plan/phase/task,
	 * helping users understand which item their messages are associated with.
	 */

	interface Props {
		label: string;
	}

	let { label }: Props = $props();
</script>

<div class="context-divider" role="separator" aria-label="Context changed to {label}">
	<span class="divider-line"></span>
	<span class="divider-label">{label}</span>
	<span class="divider-line"></span>
</div>

<style>
	.context-divider {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) 0;
		margin: var(--space-2) 0;
	}

	.divider-line {
		flex: 1;
		height: 1px;
		background: var(--color-border);
	}

	.divider-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		white-space: nowrap;
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
	}
</style>
