<script lang="ts">
	import { fetchPlanBranches, fetchRequirementFileDiff } from '$lib/api/branches';
	import type { PlanRequirementBranch, BranchDiffFile } from '$lib/api/branches';

	interface Props {
		slug: string;
	}

	let { slug }: Props = $props();

	// Requirement branches — one row per plan requirement, each with its diff
	// against base. Requirements without a branch yet (pre-execution) still
	// appear so the user sees the complete list, not only what has started.
	let branches = $state<PlanRequirementBranch[]>([]);
	let selected = $state<PlanRequirementBranch | null>(null);
	let selectedFile = $state<BranchDiffFile | null>(null);
	let patch = $state<string | null>(null);
	let loadingBranches = $state(true);
	let loadingPatch = $state(false);
	let error = $state<string | null>(null);

	// Generation counter for patch requests. If the user clicks file A, then
	// quickly clicks B, A's late response would overwrite B's unless we ignore
	// any response whose generation is no longer current.
	let patchGeneration = 0;

	// Fetch branches whenever slug changes. The $effect reruns on prop change,
	// so reparenting the component to a different plan reloads cleanly.
	$effect(() => {
		const currentSlug = slug;
		loadingBranches = true;
		error = null;
		let cancelled = false;
		fetchPlanBranches(currentSlug)
			.then((rows) => {
				if (cancelled) return;
				branches = rows;
				// Auto-select the first requirement with actual changes so the
				// user lands on something non-empty when multiple exist.
				const firstWithChanges = rows.find((r) => r.files.length > 0);
				selected = firstWithChanges ?? rows[0] ?? null;
				selectedFile = null;
				patch = null;
			})
			.catch((e) => {
				if (!cancelled) error = e instanceof Error ? e.message : 'Failed to load branches';
			})
			.finally(() => {
				if (!cancelled) loadingBranches = false;
			});
		return () => {
			cancelled = true;
		};
	});

	async function selectFile(file: BranchDiffFile) {
		if (!selected) return;
		if (selectedFile?.path === file.path) return;
		selectedFile = file;
		patch = null;
		loadingPatch = true;
		const gen = ++patchGeneration;
		try {
			const p = await fetchRequirementFileDiff(slug, selected.requirement_id, file.path);
			if (gen === patchGeneration) patch = p;
		} catch (e) {
			if (gen === patchGeneration)
				error = e instanceof Error ? e.message : 'Failed to load file diff';
		} finally {
			if (gen === patchGeneration) loadingPatch = false;
		}
	}

	function onRequirementChange(e: Event) {
		const reqId = (e.target as HTMLSelectElement).value;
		const found = branches.find((b) => b.requirement_id === reqId);
		selected = found ?? null;
		selectedFile = null;
		patch = null;
		// Invalidate any in-flight patch fetch so its response doesn't arrive
		// into the new requirement's viewer, and reset the spinner — otherwise
		// the viewer stalls on "Loading diff..." forever.
		patchGeneration++;
		loadingPatch = false;
	}

	function statusIcon(status: BranchDiffFile['status']): string {
		switch (status) {
			case 'added':
				return 'A';
			case 'deleted':
				return 'D';
			case 'renamed':
				return 'R';
			case 'copied':
				return 'C';
			case 'binary':
				return 'B';
			case 'typechange':
				return 'T';
			default:
				return 'M';
		}
	}

	function requirementLabel(b: PlanRequirementBranch): string {
		const suffix = b.files.length === 0 && !b.branch ? ' — not started' : '';
		return `[${b.requirement_id}] ${b.title}${suffix}`;
	}
</script>

{#snippet diffLine(line: string)}
	{#if line.startsWith('+++') || line.startsWith('---')}
		<span class="diff-line diff-header">{line}</span>
	{:else if line.startsWith('@@')}
		<span class="diff-line diff-hunk">{line}</span>
	{:else if line.startsWith('+')}
		<span class="diff-line diff-add">{line}</span>
	{:else if line.startsWith('-')}
		<span class="diff-line diff-del">{line}</span>
	{:else}
		<span class="diff-line">{line}</span>
	{/if}
{/snippet}

<div class="workspace" data-testid="plan-workspace">
	{#if loadingBranches}
		<div class="workspace-loading">Loading changes&hellip;</div>
	{:else if error}
		<div class="workspace-error" role="alert">{error}</div>
	{:else if branches.length === 0}
		<div class="workspace-empty">
			<p>This plan has no requirements yet. Once it's approved and execution begins, each
				requirement's branch diff will appear here.</p>
		</div>
	{:else}
		<div class="workspace-layout">
			<!-- Left: requirement selector + changed-file list -->
			<div class="workspace-tree">
				<div class="task-selector">
					<label class="selector-label" for="requirement-select">Requirement</label>
					<select
						id="requirement-select"
						data-testid="requirement-select"
						value={selected?.requirement_id ?? ''}
						onchange={onRequirementChange}
					>
						{#each branches as b}
							<option value={b.requirement_id}>{requirementLabel(b)}</option>
						{/each}
					</select>
					{#if selected}
						<div class="selector-meta" data-testid="requirement-meta">
							<span class="meta-branch" title={selected.branch || 'no branch yet'}>
								{selected.branch || '(no branch)'}
							</span>
							<span class="meta-stage meta-stage--{selected.stage || 'pending'}">
								{selected.stage || 'pending'}
							</span>
						</div>
						{#if selected.diff_error}
							<div class="selector-warning" role="alert">Diff error: {selected.diff_error}</div>
						{/if}
					{/if}
				</div>

				{#if selected === null}
					<div class="tree-empty">Select a requirement.</div>
				{:else if selected.files.length === 0}
					<div class="tree-empty" data-testid="no-changes">
						{#if !selected.branch}
							Requirement hasn't started — no branch or changes yet.
						{:else}
							No changes on this branch vs {selected.base}.
						{/if}
					</div>
				{:else}
					<div class="file-summary" data-testid="file-summary">
						{selected.files.length} file{selected.files.length === 1 ? '' : 's'} changed
						<span class="stat-add">+{selected.total_insertions}</span>
						<span class="stat-del">&minus;{selected.total_deletions}</span>
					</div>
					<nav class="tree-nav" aria-label="Changed files">
						{#each selected.files as file}
							<button
								class="tree-item tree-item--file"
								class:selected={selectedFile?.path === file.path}
								onclick={() => selectFile(file)}
								data-testid="file-{file.path}"
							>
								<span
									class="file-status file-status--{file.status}"
									title={file.status}
									aria-label={file.status}
								>
									{statusIcon(file.status)}
								</span>
								<span class="tree-name" title={file.path}>{file.path}</span>
								<span class="file-stats">
									<span class="stat-add">+{file.insertions}</span>
									<span class="stat-del">&minus;{file.deletions}</span>
								</span>
							</button>
						{/each}
					</nav>
				{/if}
			</div>

			<!-- Right: file diff viewer -->
			<div class="workspace-viewer">
				{#if selectedFile}
					<div class="viewer-header" data-testid="viewer-header">
						{selectedFile.path}
						{#if selectedFile.old_path}
							<span class="viewer-rename"> &laquo; {selectedFile.old_path}</span>
						{/if}
					</div>
				{/if}
				{#if loadingPatch}
					<div class="viewer-loading">Loading diff&hellip;</div>
				{:else if patch !== null}
					<pre class="viewer-content" data-testid="viewer-diff"><code
						>{#each patch.split('\n') as line, i}{#if i > 0}{'\n'}{/if}{@render diffLine(line)}{/each}</code
					></pre>
				{:else}
					<div class="viewer-empty">
						<p>Select a changed file to view its diff.</p>
					</div>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.workspace {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
	}

	.workspace-loading,
	.workspace-error,
	.workspace-empty {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-12) var(--space-6);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.workspace-error {
		color: var(--color-error);
	}

	.workspace-layout {
		display: flex;
		height: 100%;
		overflow: hidden;
		gap: 0;
	}

	.workspace-tree {
		width: 320px;
		flex-shrink: 0;
		border-right: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.task-selector {
		padding: var(--space-2);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.selector-label {
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.task-selector select {
		width: 100%;
		font-size: var(--font-size-xs);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-primary);
		padding: var(--space-1) var(--space-2);
	}

	.selector-meta {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		font-size: 10px;
	}

	.meta-branch {
		color: var(--color-text-muted);
		font-family: var(--font-mono, monospace);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		flex: 1;
	}

	.meta-stage {
		padding: 1px var(--space-1);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		text-transform: capitalize;
		flex-shrink: 0;
	}

	.meta-stage--completed {
		background: var(--color-success-muted, #1f3a28);
		color: var(--color-success, #4ade80);
	}

	.meta-stage--failed,
	.meta-stage--error {
		background: var(--color-error-muted, #3a1f1f);
		color: var(--color-error, #f87171);
	}

	.selector-warning {
		color: var(--color-error);
		font-size: 10px;
		padding: var(--space-1);
	}

	.tree-empty {
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.file-summary {
		padding: var(--space-1) var(--space-3);
		font-size: 10px;
		color: var(--color-text-muted);
		display: flex;
		gap: var(--space-2);
		align-items: center;
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.tree-nav {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-1);
	}

	.tree-item {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px var(--space-2);
		font-size: var(--font-size-xs);
		border-radius: var(--radius-sm);
		cursor: pointer;
		width: 100%;
		text-align: left;
		color: var(--color-text-secondary);
		border: none;
		background: none;
	}

	.tree-item--file:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.tree-item--file.selected {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.tree-name {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-family: var(--font-mono, monospace);
	}

	.file-status {
		flex-shrink: 0;
		width: 14px;
		text-align: center;
		font-family: var(--font-mono, monospace);
		font-size: 10px;
		font-weight: var(--font-weight-bold, 700);
		padding: 0 2px;
		border-radius: var(--radius-sm);
	}

	.file-status--added {
		color: var(--color-success, #4ade80);
	}

	.file-status--deleted {
		color: var(--color-error, #f87171);
	}

	.file-status--modified {
		color: var(--color-warning, #facc15);
	}

	.file-status--renamed,
	.file-status--copied {
		color: var(--color-info, #60a5fa);
	}

	.file-stats {
		flex-shrink: 0;
		display: flex;
		gap: var(--space-1);
		font-size: 10px;
		font-family: var(--font-mono, monospace);
	}

	.stat-add {
		color: var(--color-success, #4ade80);
	}

	.stat-del {
		color: var(--color-error, #f87171);
	}

	.workspace-viewer {
		flex: 1;
		overflow: auto;
		display: flex;
		flex-direction: column;
	}

	.viewer-header {
		padding: var(--space-2) var(--space-4);
		font-size: var(--font-size-xs);
		font-family: var(--font-mono, monospace);
		color: var(--color-text-muted);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.viewer-rename {
		color: var(--color-info, #60a5fa);
	}

	.viewer-loading,
	.viewer-empty {
		display: flex;
		align-items: center;
		justify-content: center;
		flex: 1;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.viewer-content {
		margin: 0;
		padding: var(--space-4);
		font-size: var(--font-size-xs);
		font-family: var(--font-mono, monospace);
		line-height: 1.6;
		color: var(--color-text-primary);
		white-space: pre;
		overflow-x: auto;
		flex: 1;
	}

	.diff-line {
		display: inline;
	}

	.diff-header {
		color: var(--color-text-muted);
		font-weight: var(--font-weight-bold, 700);
	}

	.diff-hunk {
		color: var(--color-info, #60a5fa);
	}

	.diff-add {
		color: var(--color-success, #4ade80);
		background: rgba(74, 222, 128, 0.06);
	}

	.diff-del {
		color: var(--color-error, #f87171);
		background: rgba(248, 113, 113, 0.06);
	}
</style>
