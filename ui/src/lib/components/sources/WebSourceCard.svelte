<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { WebSource } from '$lib/types/source';
	import { STATUS_META } from '$lib/types/source';

	interface Props {
		source: WebSource;
		compact?: boolean;
		onclick?: () => void;
	}

	let { source, compact = false, onclick }: Props = $props();

	const statusMeta = $derived(STATUS_META[source.status]);

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleDateString();
	}

	function formatRelativeTime(dateStr: string): string {
		const now = new Date();
		const then = new Date(dateStr);
		const diffMs = now.getTime() - then.getTime();
		const diffMins = Math.floor(diffMs / 60000);
		const diffHours = Math.floor(diffMins / 60);
		const diffDays = Math.floor(diffHours / 24);

		if (diffMins < 1) return 'just now';
		if (diffMins < 60) return `${diffMins}m ago`;
		if (diffHours < 24) return `${diffHours}h ago`;
		if (diffDays < 7) return `${diffDays}d ago`;
		return formatDate(dateStr);
	}

	/**
	 * Truncates a URL while preserving the domain for context.
	 * For long URLs, shows domain + truncated path.
	 */
	function truncateUrl(url: string, maxLength: number = 60): string {
		if (url.length <= maxLength) return url;

		try {
			const parsed = new URL(url);
			const domain = parsed.hostname;
			const path = parsed.pathname + parsed.search;

			// If just the domain is too long, truncate it
			if (domain.length + 10 > maxLength) {
				return domain.substring(0, maxLength - 3) + '...';
			}

			// Calculate how much path we can show
			const remainingLength = maxLength - domain.length - 4; // 4 for '...' and '/'
			if (remainingLength > 5 && path.length > remainingLength) {
				return domain + path.substring(0, remainingLength) + '...';
			}

			return domain + path.substring(0, remainingLength);
		} catch {
			// Fallback to simple truncation for invalid URLs
			return url.substring(0, maxLength - 3) + '...';
		}
	}
</script>

<button
	class="web-card"
	class:compact
	onclick={onclick}
	type="button"
	aria-label="View web source: {source.title || source.name}"
>
	<div class="web-icon">
		<Icon name="globe" size={compact ? 16 : 20} />
	</div>

	<div class="web-content">
		<div class="web-header">
			<span class="web-name">{source.title || source.name}</span>
			<span class="type-badge" aria-label="Source type: Web">
				<Icon name="globe" size={10} />
				Web
			</span>
		</div>

		{#if !compact}
			<div class="web-meta">
				<span
					class="status-badge"
					style="color: {statusMeta.color}"
					aria-label="Status: {statusMeta.label}"
				>
					<Icon name={statusMeta.icon} size={12} />
					{statusMeta.label}
				</span>
				{#if source.chunkCount !== undefined}
					<span class="chunk-count" aria-label="{source.chunkCount} content chunks">
						<Icon name="layers" size={12} />
						{source.chunkCount} chunks
					</span>
				{/if}
				{#if source.autoRefresh}
					<span class="auto-refresh" aria-label="Auto-refresh enabled">
						<Icon name="refresh-cw" size={12} />
						Auto
					</span>
				{/if}
				{#if source.project}
					<span class="project-tag" aria-label="Project: {source.project}">
						<Icon name="folder" size={12} />
						{source.project}
					</span>
				{/if}
			</div>

			<div class="web-footer">
				<span class="url" title={source.url}>
					<Icon name="link" size={10} />
					{truncateUrl(source.url)}
				</span>
				<div class="footer-right">
					{#if source.lastFetched}
						<span class="last-fetched" title="Last fetched: {new Date(source.lastFetched).toLocaleString()}">
							<Icon name="clock" size={10} />
							{formatRelativeTime(source.lastFetched)}
						</span>
					{/if}
					<span class="added-at" title="Added on {new Date(source.addedAt).toLocaleString()}">{formatDate(source.addedAt)}</span>
				</div>
			</div>
		{/if}
	</div>

	<div class="web-arrow">
		<Icon name="chevron-right" size={16} />
	</div>
</button>

<style>
	.web-card {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
		text-align: left;
		width: 100%;
	}

	.web-card:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent-muted);
	}

	.web-card:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.web-card.compact {
		padding: var(--space-2) var(--space-3);
		align-items: center;
	}

	.web-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		margin-top: 2px;
		color: var(--color-info);
	}

	.web-content {
		flex: 1;
		min-width: 0;
	}

	.web-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.web-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.type-badge {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		background: var(--color-info)20;
		color: var(--color-info);
		text-transform: uppercase;
		font-weight: var(--font-weight-medium);
	}

	.web-meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		margin-top: var(--space-2);
	}

	.status-badge {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
	}

	.chunk-count,
	.auto-refresh,
	.project-tag {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.auto-refresh {
		color: var(--color-success);
	}

	.web-footer {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-top: var(--space-2);
		gap: var(--space-2);
	}

	.url {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-mono);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		flex: 1;
		min-width: 0;
	}

	.footer-right {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-shrink: 0;
	}

	.last-fetched {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.added-at {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.web-arrow {
		color: var(--color-text-muted);
		flex-shrink: 0;
		align-self: center;
	}
</style>
