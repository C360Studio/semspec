<script lang="ts">
	import {
		fetchPhaseArtifactContent,
		fetchPhaseArtifacts,
		phaseArtifactLabel,
		type PhaseArtifact,
		type PhaseArtifactName
	} from '$lib/api/artifacts';
	import { artifactSectionId, renderArtifact } from './artifactRenderer';

	interface Props {
		slug: string;
	}

	let { slug }: Props = $props();

	let artifacts = $state<PhaseArtifact[]>([]);
	let contents = $state<Record<string, string>>({});
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Artifact contents arrive in parallel after the list. The renderer needs
	// the markdown to produce HTML + headings; cache the rendered form keyed
	// by name so subsequent reactive reads don't reparse.
	let rendered = $derived.by(() => {
		const out: Record<string, ReturnType<typeof renderArtifact>> = {};
		for (const a of artifacts) {
			const md = contents[a.name];
			if (md !== undefined) {
				out[a.name] = renderArtifact(a.name, md);
			}
		}
		return out;
	});

	// Whenever the slug changes, refetch from scratch. `cancelled` guards
	// against late responses from the previous slug overwriting the new
	// view's state — a real risk with parallel content fetches.
	$effect(() => {
		const currentSlug = slug;
		let cancelled = false;
		loading = true;
		error = null;
		artifacts = [];
		contents = {};

		(async () => {
			try {
				const list = await fetchPhaseArtifacts(currentSlug);
				if (cancelled) return;
				artifacts = list.artifacts;
				const results = await Promise.all(
					list.artifacts.map(async (a) => {
						const body = await fetchPhaseArtifactContent(currentSlug, a.name);
						return [a.name, body] as const;
					})
				);
				if (cancelled) return;
				const next: Record<string, string> = {};
				for (const [name, body] of results) {
					if (body !== null) next[name] = body;
				}
				contents = next;
			} catch (e) {
				if (!cancelled) {
					error = e instanceof Error ? e.message : 'Failed to load artifacts';
				}
			} finally {
				if (!cancelled) loading = false;
			}
		})();

		return () => {
			cancelled = true;
		};
	});

	function scrollToArtifact(name: PhaseArtifactName) {
		const id = artifactSectionId(name);
		const el = document.getElementById(id);
		if (el) {
			el.scrollIntoView({ behavior: 'smooth', block: 'start' });
		}
	}
</script>

<div class="artifacts" data-testid="phase-artifacts">
	{#if loading}
		<div class="artifacts-loading">Loading artifacts&hellip;</div>
	{:else if error}
		<div class="artifacts-error" role="alert">{error}</div>
	{:else if artifacts.length === 0}
		<div class="artifacts-empty">
			<p>No phase artifacts written yet. They appear here as the plan progresses
				through architecture, requirements, scenarios, and QA.</p>
		</div>
	{:else}
		<nav class="artifacts-toc" aria-label="Artifact sections">
			{#each artifacts as a (a.name)}
				<button
					type="button"
					class="toc-chip"
					onclick={() => scrollToArtifact(a.name)}
					data-testid="toc-{a.name}"
				>
					{phaseArtifactLabel(a.name)}
				</button>
			{/each}
		</nav>

		{#each artifacts as a (a.name)}
			{@const r = rendered[a.name]}
			{@const sectionId = artifactSectionId(a.name)}
			{@const titleId = `${sectionId}-title`}
			<section
				class="artifact-section"
				id={sectionId}
				aria-labelledby={titleId}
				data-testid="artifact-{a.name}"
			>
				<header class="artifact-header">
					<h2 id={titleId} class="artifact-title">{phaseArtifactLabel(a.name)}</h2>
					<span class="artifact-filename">{a.filename}</span>
				</header>
				{#if r}
					<div class="artifact-body markdown-body">
						{@html r.html}
					</div>
				{:else}
					<div class="artifact-body-loading">Loading&hellip;</div>
				{/if}
			</section>
		{/each}
	{/if}
</div>

<style>
	.artifacts {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-2) 0;
	}

	.artifacts-loading,
	.artifacts-error,
	.artifacts-empty {
		padding: var(--space-8) var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.artifacts-error {
		color: var(--color-error);
	}

	.artifacts-toc {
		position: sticky;
		top: 0;
		z-index: 1;
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.toc-chip {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: background var(--transition-fast);
	}

	.toc-chip:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.artifact-section {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
		scroll-margin-top: var(--space-12);
	}

	.artifact-header {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
	}

	.artifact-title {
		font-size: var(--font-size-md);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.artifact-filename {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-mono, monospace);
	}

	.artifact-body {
		padding: var(--space-4);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		line-height: 1.6;
	}

	.artifact-body-loading {
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	/* Markdown body styles — scoped via :global because @html content
	   bypasses Svelte's CSS scoping. Keep selectors anchored at
	   .markdown-body so we never bleed onto the rest of the app. */
	:global(.markdown-body h1),
	:global(.markdown-body h2),
	:global(.markdown-body h3),
	:global(.markdown-body h4),
	:global(.markdown-body h5),
	:global(.markdown-body h6) {
		margin: var(--space-4) 0 var(--space-2);
		color: var(--color-text-primary);
		font-weight: var(--font-weight-semibold);
		scroll-margin-top: var(--space-12);
	}

	:global(.markdown-body h1) {
		font-size: var(--font-size-lg);
	}
	:global(.markdown-body h2) {
		font-size: var(--font-size-md);
	}
	:global(.markdown-body h3) {
		font-size: var(--font-size-sm);
		text-transform: uppercase;
		letter-spacing: 0.02em;
		color: var(--color-text-secondary);
	}

	:global(.markdown-body p) {
		margin: var(--space-2) 0;
	}

	:global(.markdown-body ul),
	:global(.markdown-body ol) {
		margin: var(--space-2) 0;
		padding-left: var(--space-6);
	}

	:global(.markdown-body li) {
		margin: var(--space-1) 0;
	}

	:global(.markdown-body code) {
		font-family: var(--font-mono, monospace);
		font-size: 0.92em;
		padding: 1px 4px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
	}

	:global(.markdown-body pre) {
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		overflow-x: auto;
	}

	:global(.markdown-body pre code) {
		padding: 0;
		background: none;
	}

	:global(.markdown-body table) {
		border-collapse: collapse;
		margin: var(--space-2) 0;
		font-size: var(--font-size-xs);
		width: 100%;
	}

	:global(.markdown-body th),
	:global(.markdown-body td) {
		border: 1px solid var(--color-border);
		padding: var(--space-1) var(--space-2);
		text-align: left;
		vertical-align: top;
	}

	:global(.markdown-body th) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		font-weight: var(--font-weight-semibold);
	}

	:global(.markdown-body a) {
		color: var(--color-accent);
		text-decoration: none;
	}

	:global(.markdown-body a:hover) {
		text-decoration: underline;
	}

	:global(.markdown-body blockquote) {
		margin: var(--space-2) 0;
		padding-left: var(--space-3);
		border-left: 3px solid var(--color-border);
		color: var(--color-text-secondary);
	}
</style>
