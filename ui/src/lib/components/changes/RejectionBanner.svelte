<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { TaskRejection } from '$lib/types/task';
	import { getRejectionRouting } from '$lib/types/task';

	interface Props {
		rejection: TaskRejection;
		taskDescription?: string;
	}

	let { rejection, taskDescription }: Props = $props();

	const routing = $derived(getRejectionRouting(rejection.type));

	function getIcon(): string {
		switch (routing.action) {
			case 'retry':
				return 'refresh-cw';
			case 'plan':
				return 'arrow-left';
			case 'decompose':
				return 'scissors';
			default:
				return 'alert-triangle';
		}
	}
</script>

<div class="rejection-banner" data-action={routing.action}>
	<div class="banner-icon">
		<Icon name={getIcon()} size={20} />
	</div>
	<div class="banner-content">
		<div class="banner-header">
			<span class="rejection-type">{routing.label}</span>
			<span class="iteration">Iteration {rejection.iteration}</span>
		</div>
		{#if taskDescription}
			<p class="task-description">{taskDescription}</p>
		{/if}
		<p class="rejection-reason">{rejection.reason}</p>
		<p class="routing-info">{routing.description}</p>
	</div>
</div>

<style>
	.rejection-banner {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-4);
		border-radius: var(--radius-lg);
		border: 1px solid;
	}

	.rejection-banner[data-action='retry'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		border-color: var(--color-warning);
	}

	.rejection-banner[data-action='retry'] .banner-icon {
		color: var(--color-warning);
	}

	.rejection-banner[data-action='plan'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-color: var(--color-error);
	}

	.rejection-banner[data-action='plan'] .banner-icon {
		color: var(--color-error);
	}

	.rejection-banner[data-action='decompose'] {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
	}

	.rejection-banner[data-action='decompose'] .banner-icon {
		color: var(--color-accent);
	}

	.banner-icon {
		flex-shrink: 0;
		padding-top: 2px;
	}

	.banner-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.banner-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.rejection-type {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.iteration {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	.task-description {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		font-style: italic;
	}

	.rejection-reason {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.routing-info {
		margin: 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
