<script lang="ts">
	interface Props {
		status: string;
		size?: 'sm' | 'md';
	}

	let { status, size = 'sm' }: Props = $props();

	const statusConfig: Record<string, { class: string; label: string }> = {
		executing: { class: 'info', label: 'Executing' },
		exploring: { class: 'info', label: 'Exploring' },
		paused: { class: 'warning', label: 'Paused' },
		awaiting_approval: { class: 'warning', label: 'Review' },
		complete: { class: 'success', label: 'Complete' },
		approved: { class: 'success', label: 'Approved' },
		implementing: { class: 'info', label: 'Implementing' },
		drafted: { class: 'neutral', label: 'Drafted' },
		failed: { class: 'error', label: 'Failed' },
		cancelled: { class: 'neutral', label: 'Cancelled' }
	};

	const config = $derived(statusConfig[status] || { class: 'neutral', label: status });
</script>

<span class="badge badge-{config.class}" class:sm={size === 'sm'}>
	{config.label}
</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-full);
		text-transform: capitalize;
	}

	.badge.sm {
		padding: 2px var(--space-2);
		font-size: 10px;
	}

	.badge-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.badge-warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.badge-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.badge-info {
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.badge-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}
</style>
