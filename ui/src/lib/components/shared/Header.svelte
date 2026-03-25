<script lang="ts">
	import Icon from './Icon.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { setupStore } from '$lib/stores/setup.svelte';

	interface Props {
		activeLoopCount?: number;
	}

	let { activeLoopCount = 0 }: Props = $props();
</script>

<header class="header">
	<div class="header-content">
		<div class="project-info">
			{#if setupStore.status?.project_name}
				<span class="project-name">{setupStore.status.project_name}</span>
				{#if setupStore.status.project_description}
					<span class="project-description">{setupStore.status.project_description}</span>
				{/if}
			{/if}
		</div>
		<div class="header-status">
			{#if activeLoopCount > 0}
				<div class="status-item loops">
					<Icon name="activity" size={14} />
					<span>{activeLoopCount} active loop{activeLoopCount !== 1 ? 's' : ''}</span>
				</div>
			{/if}
			<div class="status-item connection" class:connected={activityStore.connected}>
				<span class="status-dot"></span>
				<span class="status-text">{activityStore.connected ? 'Connected' : 'Disconnected'}</span>
			</div>
			<a href="/settings" class="settings-link" title="Settings">
				<Icon name="settings" size={16} />
			</a>
		</div>
	</div>
</header>

<style>
	.header {
		height: var(--header-height);
		background: var(--color-bg-secondary);
		border-bottom: 1px solid var(--color-border);
		display: flex;
		align-items: center;
		padding: 0 var(--space-4);
	}

	.header-content {
		display: flex;
		align-items: center;
		justify-content: space-between;
		width: 100%;
	}

	.project-info {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
		overflow: hidden;
	}

	.project-name {
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.project-description {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	@media (max-width: 768px) {
		.project-description {
			display: none;
		}
	}

	.header-status {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.status-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.status-item.loops {
		color: var(--color-accent);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.connection.connected .status-dot {
		background: var(--color-success);
	}

	.status-text {
		color: var(--color-text-muted);
	}

	.settings-link {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		transition: all var(--transition-fast);
	}

	.settings-link:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}
</style>
