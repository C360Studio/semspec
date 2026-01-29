<script lang="ts">
	import Icon from './Icon.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';

	interface Props {
		currentPath: string;
	}

	let { currentPath }: Props = $props();

	const navItems = [
		{ path: '/', icon: 'message-square', label: 'Chat' },
		{ path: '/dashboard', icon: 'layout-dashboard', label: 'Dashboard' },
		{ path: '/tasks', icon: 'list-checks', label: 'Tasks' },
		{ path: '/history', icon: 'history', label: 'History' },
		{ path: '/settings', icon: 'settings', label: 'Settings' }
	];

	function isActive(path: string): boolean {
		if (path === '/') return currentPath === '/';
		return currentPath.startsWith(path);
	}
</script>

<aside class="sidebar">
	<div class="sidebar-header">
		<span class="logo">Semspec</span>
	</div>

	<nav class="sidebar-nav" aria-label="Main navigation">
		{#each navItems as item}
			<a
				href={item.path}
				class="nav-item"
				class:active={isActive(item.path)}
				aria-current={isActive(item.path) ? 'page' : undefined}
			>
				<Icon name={item.icon} size={20} />
				<span>{item.label}</span>

				{#if item.path === '/tasks' && loopsStore.pendingReview.length > 0}
					<span class="badge" aria-label="{loopsStore.pendingReview.length} pending reviews">
						{loopsStore.pendingReview.length}
					</span>
				{/if}
			</a>
		{/each}
	</nav>

	<div class="sidebar-footer">
		<div class="system-status" role="status" aria-live="polite">
			<div class="status-indicator" class:healthy={systemStore.healthy} aria-hidden="true"></div>
			<span class="status-text">
				{systemStore.healthy ? 'System healthy' : 'System issues'}
			</span>
		</div>

		<div class="active-loops" role="status">
			<Icon name="activity" size={14} />
			<span>{loopsStore.active.length} active loops</span>
		</div>
	</div>
</aside>

<style>
	.sidebar {
		width: var(--sidebar-width);
		height: 100%;
		background: var(--color-bg-secondary);
		border-right: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		flex-shrink: 0;
	}

	.sidebar-header {
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.logo {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.sidebar-nav {
		flex: 1;
		padding: var(--space-2);
	}

	.nav-item {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-3);
		color: var(--color-text-secondary);
		border-radius: var(--radius-md);
		text-decoration: none;
		transition: all var(--transition-fast);
	}

	.nav-item:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
		text-decoration: none;
	}

	.nav-item.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.nav-item .badge {
		margin-left: auto;
		background: var(--color-warning);
		color: var(--color-bg-primary);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		padding: 2px 6px;
		border-radius: var(--radius-full);
	}

	.sidebar-footer {
		padding: var(--space-4);
		border-top: 1px solid var(--color-border);
		font-size: var(--font-size-sm);
	}

	.system-status {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-2);
	}

	.status-indicator {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.status-indicator.healthy {
		background: var(--color-success);
	}

	.status-text {
		color: var(--color-text-muted);
	}

	.active-loops {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-muted);
	}
</style>
