<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { RepositorySource } from '$lib/types/source';
	import { STATUS_META, LANGUAGE_META } from '$lib/types/source';

	interface Props {
		source: RepositorySource;
		compact?: boolean;
		onclick?: () => void;
	}

	let { source, compact = false, onclick }: Props = $props();

	const statusMeta = $derived(STATUS_META[source.status]);

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleDateString();
	}

	function formatCommit(commit: string): string {
		return commit.substring(0, 7);
	}

	function getLanguageColor(lang: string): string {
		return LANGUAGE_META[lang]?.color || 'var(--color-text-muted)';
	}

	function getLanguageName(lang: string): string {
		return LANGUAGE_META[lang]?.name || lang;
	}
</script>

<button
	class="repo-card"
	class:compact
	onclick={onclick}
	onkeydown={(e) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			onclick?.();
		}
	}}
	type="button"
	aria-label="View repository {source.name}"
>
	<div class="repo-icon">
		<Icon name="git-branch" size={compact ? 16 : 20} />
	</div>

	<div class="repo-content">
		<div class="repo-header">
			<span class="repo-name">{source.name}</span>
			<span class="type-badge">
				<Icon name="git-branch" size={10} />
				Repository
			</span>
		</div>

		{#if !compact}
			<div class="repo-meta">
				<span class="status-badge" style="color: {statusMeta.color}">
					<Icon name={statusMeta.icon} size={12} />
					{statusMeta.label}
				</span>
				{#if source.branch}
					<span class="branch">
						<Icon name="git-commit" size={12} />
						{source.branch}
					</span>
				{/if}
				{#if source.entityCount !== undefined}
					<span class="entity-count">
						<Icon name="code" size={12} />
						{source.entityCount} entities
					</span>
				{/if}
				{#if source.project}
					<span class="project-tag">
						<Icon name="folder" size={12} />
						{source.project}
					</span>
				{/if}
			</div>

			{#if source.languages && source.languages.length > 0}
				<div class="languages">
					{#each source.languages.slice(0, 5) as lang}
						<span class="language-badge" style="background: {getLanguageColor(lang)}20; color: {getLanguageColor(lang)}">
							{getLanguageName(lang)}
						</span>
					{/each}
					{#if source.languages.length > 5}
						<span class="language-more">+{source.languages.length - 5}</span>
					{/if}
				</div>
			{/if}

			<div class="repo-footer">
				<span class="url">{source.url}</span>
				<div class="footer-right">
					{#if source.lastCommit}
						<span class="commit" title="Last commit">
							<Icon name="git-commit" size={10} />
							{formatCommit(source.lastCommit)}
						</span>
					{/if}
					<span class="added-at">{formatDate(source.addedAt)}</span>
				</div>
			</div>
		{/if}
	</div>

	<div class="repo-arrow">
		<Icon name="chevron-right" size={16} />
	</div>
</button>

<style>
	.repo-card {
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

	.repo-card:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent-muted);
	}

	.repo-card.compact {
		padding: var(--space-2) var(--space-3);
		align-items: center;
	}

	.repo-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		margin-top: 2px;
		color: var(--color-accent);
	}

	.repo-content {
		flex: 1;
		min-width: 0;
	}

	.repo-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.repo-name {
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
		background: var(--color-accent-muted);
		color: var(--color-accent);
		text-transform: uppercase;
		font-weight: var(--font-weight-medium);
	}

	.repo-meta {
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

	.branch,
	.entity-count,
	.project-tag {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.languages {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		margin-top: var(--space-2);
	}

	.language-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		font-weight: var(--font-weight-medium);
	}

	.language-more {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		color: var(--color-text-muted);
	}

	.repo-footer {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-top: var(--space-2);
		gap: var(--space-2);
	}

	.url {
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

	.commit {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-mono);
	}

	.added-at {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.repo-arrow {
		color: var(--color-text-muted);
		flex-shrink: 0;
		align-self: center;
	}
</style>
