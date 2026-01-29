<script lang="ts">
	import { activityStore } from '$lib/stores/activity.svelte';

	// Minimal header - can expand later with search, user info, etc.
</script>

<header class="header">
	<div class="header-content">
		<div class="connection-status" class:connected={activityStore.connected}>
			<span class="status-dot"></span>
			<span class="status-text">{activityStore.connected ? 'Connected' : 'Disconnected'}</span>
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
		justify-content: flex-end;
		width: 100%;
	}

	.connection-status {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.connection-status.connected .status-dot {
		background: var(--color-success);
	}

	.status-text {
		color: var(--color-text-muted);
	}
</style>
