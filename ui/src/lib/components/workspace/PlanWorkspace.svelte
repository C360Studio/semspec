<script lang="ts">
	import { fetchWorkspaceTasks, fetchWorkspaceTree, fetchWorkspaceFile } from '$lib/api/workspace';
	import type { WorkspaceTask, WorkspaceEntry } from '$lib/api/workspace';

	interface Props {
		slug: string;
	}

	let { slug }: Props = $props();

	let tasks = $state<WorkspaceTask[]>([]);
	let selectedTask = $state<WorkspaceTask | null>(null);
	let tree = $state<WorkspaceEntry[]>([]);
	let selectedPath = $state<string | null>(null);
	let fileContent = $state<string | null>(null);
	let loadingTasks = $state(true);
	let loadingTree = $state(false);
	let loadingFile = $state(false);
	let error = $state<string | null>(null);

	function formatSize(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	// Fetch workspace tasks for this plan — re-runs when slug changes
	$effect(() => {
		const currentSlug = slug; // track slug synchronously for reactivity
		loadingTasks = true;
		error = null;
		fetchWorkspaceTasks()
			.then((all) => {
				tasks = all.filter((t) => t.branch.includes(currentSlug));
				if (tasks.length > 0) {
					selectedTask = tasks[0];
				}
			})
			.catch((e) => {
				error = e instanceof Error ? e.message : 'Failed to load workspace tasks';
			})
			.finally(() => {
				loadingTasks = false;
			});
	});

	// Load tree when selectedTask changes — cancels stale requests
	$effect(() => {
		const task = selectedTask;
		if (!task) {
			tree = [];
			return;
		}
		let cancelled = false;
		loadingTree = true;
		fetchWorkspaceTree(task.task_id)
			.then((entries) => {
				if (!cancelled) tree = entries;
			})
			.catch((e) => {
				if (!cancelled) error = e instanceof Error ? e.message : 'Failed to load file tree';
			})
			.finally(() => {
				if (!cancelled) loadingTree = false;
			});
		return () => { cancelled = true; };
	});

	async function selectFile(taskId: string, path: string) {
		if (selectedPath === path) return;
		selectedPath = path;
		fileContent = null;
		loadingFile = true;
		try {
			fileContent = await fetchWorkspaceFile(taskId, path);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load file';
		} finally {
			loadingFile = false;
		}
	}
</script>

{#snippet treeNode(entry: WorkspaceEntry, taskId: string)}
	{#if entry.is_dir}
		<details class="tree-dir">
			<summary class="tree-item tree-item--dir">
				<span class="tree-icon">&#128193;</span>
				<span class="tree-name">{entry.name}</span>
			</summary>
			{#if entry.children}
				<div class="tree-children">
					{#each entry.children as child}
						{@render treeNode(child, taskId)}
					{/each}
				</div>
			{/if}
		</details>
	{:else}
		<button
			class="tree-item tree-item--file"
			class:selected={selectedPath === entry.path}
			onclick={() => selectFile(taskId, entry.path)}
		>
			<span class="tree-icon">&#128196;</span>
			<span class="tree-name">{entry.name}</span>
			{#if entry.size > 0}
				<span class="tree-size">{formatSize(entry.size)}</span>
			{/if}
		</button>
	{/if}
{/snippet}

<div class="workspace">
	{#if loadingTasks}
		<div class="workspace-loading">Loading workspace...</div>
	{:else if error}
		<div class="workspace-error" role="alert">{error}</div>
	{:else if tasks.length === 0}
		<div class="workspace-empty">
			<p>No workspace files yet. Files will appear here when execution produces artifacts.</p>
		</div>
	{:else}
		<div class="workspace-layout">
			<!-- Left: task selector + file tree -->
			<div class="workspace-tree">
				{#if tasks.length > 1}
					<div class="task-selector">
						<select
							value={selectedTask?.task_id ?? ''}
							onchange={(e) => {
								const found = tasks.find((t) => t.task_id === (e.target as HTMLSelectElement).value);
								selectedTask = found ?? null;
								selectedPath = null;
								fileContent = null;
							}}
						>
							{#each tasks as task}
								<option value={task.task_id}>{task.branch} ({task.file_count} files)</option>
							{/each}
						</select>
					</div>
				{/if}

				{#if loadingTree}
					<div class="tree-loading">Loading tree...</div>
				{:else if tree.length === 0}
					<div class="tree-empty">No files in this workspace.</div>
				{:else if selectedTask}
					<nav class="tree-nav" aria-label="File tree">
						{#each tree as entry}
							{@render treeNode(entry, selectedTask.task_id)}
						{/each}
					</nav>
				{/if}
			</div>

			<!-- Right: file content viewer -->
			<div class="workspace-viewer">
				{#if selectedPath}
					<div class="viewer-header">{selectedPath}</div>
				{/if}
				{#if loadingFile}
					<div class="viewer-loading">Loading file...</div>
				{:else if fileContent !== null}
					<pre class="viewer-content"><code>{fileContent}</code></pre>
				{:else}
					<div class="viewer-empty">
						<p>Select a file to view its contents.</p>
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
		width: 240px;
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

	.tree-loading,
	.tree-empty {
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.tree-nav {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-2);
	}

	.tree-dir {
		margin: 0;
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

	.tree-item--dir {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		list-style: none;
	}

	.tree-item--file:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.tree-item--file.selected {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.tree-icon {
		font-size: 12px;
		flex-shrink: 0;
	}

	.tree-name {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.tree-size {
		flex-shrink: 0;
		color: var(--color-text-muted);
		font-size: 10px;
	}

	.tree-children {
		padding-left: var(--space-3);
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
		white-space: pre-wrap;
		word-break: break-all;
		flex: 1;
	}
</style>
