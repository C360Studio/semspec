<script lang="ts">
	/**
	 * Workspace Page - Browse task worktree files from the sandbox store.
	 *
	 * Two views:
	 * 1. Task selector — shows tasks that have isolated worktrees
	 * 2. File browser — tree + preview for a selected task's files
	 *
	 * URL param ?task_id=X auto-selects a task on load.
	 */

	import { onMount, untrack } from 'svelte';
	import {
		fetchWorkspaceTasks,
		fetchWorkspaceTree,
		fetchWorkspaceFile,
		getWorkspaceDownloadUrl
	} from '$lib/api/workspace';
	import type { WorkspaceTask, WorkspaceEntry } from '$lib/api/workspace';

	interface Props {
		data: { tasks: WorkspaceTask[]; taskIdParam: string | null };
	}

	let { data }: Props = $props();

	// Task list state — initialized from load function; overwritten by manual Refresh.
	// untrack: one-shot seed from load data — loadTasks() handles subsequent refreshes.
	let tasks = $state<WorkspaceTask[]>(untrack(() => data.tasks));
	let tasksLoading = $state(false);
	let tasksError = $state<string | null>(null);

	// Selected task
	let selectedTaskId = $state<string | null>(null);

	// File tree state
	let tree = $state<WorkspaceEntry[]>([]);
	let treeLoading = $state(false);
	let treeError = $state<string | null>(null);

	// File preview state
	let selectedPath = $state<string | null>(null);
	let selectedEntry = $state<WorkspaceEntry | null>(null);
	let fileContent = $state<string | null>(null);
	let fileLoading = $state(false);
	let fileError = $state<string | null>(null);

	// Track expanded directories in the file tree
	let expanded = $state(new Set<string>());

	// Copy-to-clipboard state
	let copied = $state(false);
	let copiedPath = $state(false);

	// Auto-select task from URL param (browser-only, needs DOM interaction)
	onMount(() => {
		if (data.taskIdParam) {
			selectTask(data.taskIdParam);
		}
	});

	async function loadTasks() {
		tasksLoading = true;
		tasksError = null;

		try {
			tasks = await fetchWorkspaceTasks();
		} catch (err) {
			tasksError = err instanceof Error ? err.message : 'Failed to load workspace';
		} finally {
			tasksLoading = false;
		}
	}

	async function selectTask(taskId: string) {
		selectedTaskId = taskId;
		selectedPath = null;
		selectedEntry = null;
		fileContent = null;
		fileError = null;
		expanded = new Set();
		treeLoading = true;
		treeError = null;

		try {
			tree = await fetchWorkspaceTree(taskId);
		} catch (err) {
			treeError = err instanceof Error ? err.message : 'Failed to load worktree';
		} finally {
			treeLoading = false;
		}
	}

	function backToTasks() {
		selectedTaskId = null;
		tree = [];
		selectedPath = null;
		selectedEntry = null;
		fileContent = null;
	}

	async function selectFile(entry: WorkspaceEntry) {
		if (entry.is_dir) {
			toggleDir(entry.path);
			return;
		}

		if (!selectedTaskId) return;

		selectedPath = entry.path;
		selectedEntry = entry;
		fileContent = null;
		fileError = null;
		fileLoading = true;
		copied = false;
		copiedPath = false;

		try {
			fileContent = await fetchWorkspaceFile(selectedTaskId, entry.path);
		} catch (err) {
			const message = err instanceof Error ? err.message : 'Failed to load file';
			// Surface friendly messages for known status codes
			if (message.includes('415') || message.includes('Unsupported Media')) {
				fileError = 'Binary file — preview not available';
			} else if (message.includes('413') || message.includes('too large')) {
				fileError = 'File too large to preview (max 1 MB)';
			} else {
				fileError = message;
			}
		} finally {
			fileLoading = false;
		}
	}

	function toggleDir(path: string) {
		const next = new Set(expanded);
		if (next.has(path)) {
			next.delete(path);
		} else {
			next.add(path);
		}
		expanded = next;
	}

	function handleTreeKeyDown(e: KeyboardEvent, entry: WorkspaceEntry) {
		if (e.key === 'ArrowRight' && entry.is_dir && !expanded.has(entry.path)) {
			e.preventDefault();
			toggleDir(entry.path);
		} else if (e.key === 'ArrowLeft' && entry.is_dir && expanded.has(entry.path)) {
			e.preventDefault();
			toggleDir(entry.path);
		} else if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			selectFile(entry);
		}
	}

	function formatSize(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	function fileIcon(entry: WorkspaceEntry): string {
		if (entry.is_dir) return expanded.has(entry.path) ? 'v' : '>';
		const ext = fileExtension(entry.name);
		if (['go', 'py', 'js', 'ts', 'svelte', 'rs'].includes(ext)) return '#';
		if (['md', 'txt', 'log'].includes(ext)) return '=';
		if (['json', 'yaml', 'yml', 'toml'].includes(ext)) return '{';
		return '.';
	}

	function fileExtension(name: string): string {
		const dot = name.lastIndexOf('.');
		return dot > 0 ? name.substring(dot + 1).toLowerCase() : '';
	}

	function countFiles(entries: WorkspaceEntry[]): number {
		let count = 0;
		for (const e of entries) {
			if (e.is_dir && e.children) {
				count += countFiles(e.children);
			} else if (!e.is_dir) {
				count++;
			}
		}
		return count;
	}

	const totalFiles = $derived(countFiles(tree));

	const selectedTaskTitle = $derived(selectedTaskId ?? '');

	async function copyToClipboard(text: string, isPath = false) {
		await navigator.clipboard.writeText(text);
		if (isPath) {
			copiedPath = true;
			setTimeout(() => (copiedPath = false), 2000);
		} else {
			copied = true;
			setTimeout(() => (copied = false), 2000);
		}
	}
</script>

<svelte:head>
	<title>Workspace - Semspec</title>
</svelte:head>

<div class="workspace-page">
	{#if !selectedTaskId}
		<!-- Task list view -->
		<header class="workspace-header">
			<h1>Workspace</h1>
			{#if !tasksLoading && !tasksError}
				<span class="file-count">{tasks.length} tasks</span>
			{/if}
			<button
				class="action-btn"
				onclick={loadTasks}
				aria-label="Refresh task list"
				disabled={tasksLoading}
			>
				&#x21bb;
			</button>
		</header>

		{#if tasksLoading}
			<div class="loading-state">
				<p>Loading workspace...</p>
			</div>
		{:else if tasksError}
			<div class="error-state" role="alert">
				<p>{tasksError}</p>
				<button class="retry-btn" onclick={loadTasks}>Retry</button>
			</div>
		{:else if tasks.length === 0}
			<div class="empty-state" data-testid="workspace-empty">
				<div class="empty-icon">W</div>
				<h2>No Active Worktrees</h2>
				<p>Tasks with sandbox isolation will appear here.</p>
			</div>
		{:else}
			<div class="task-list" data-testid="workspace-task-list">
				{#each tasks as task (task.task_id)}
					<button
						class="task-card"
						onclick={() => selectTask(task.task_id)}
						data-testid="workspace-task-{task.task_id}"
					>
						<div class="task-card-body">
							<span class="task-card-title">{task.task_id}</span>
							<div class="task-card-meta">
								<span>{task.file_count} files</span>
								{#if task.branch}
									<span class="task-card-branch">{task.branch}</span>
								{/if}
							</div>
						</div>
					</button>
				{/each}
			</div>
		{/if}
	{:else}
		<!-- File browser view -->
		<header class="workspace-header">
			<button class="action-btn" onclick={backToTasks} aria-label="Back to task list">
				&larr;
			</button>
			<h1 class="header-title">{selectedTaskTitle}</h1>
			{#if !treeLoading && !treeError}
				<span class="file-count">{totalFiles} files</span>
			{/if}
			<a
				class="action-btn"
				href={getWorkspaceDownloadUrl(selectedTaskId)}
				download
				aria-label="Download all files as ZIP"
				title="Download ZIP"
			>
				ZIP
			</a>
			<button
				class="action-btn"
				onclick={() => selectTask(selectedTaskId!)}
				aria-label="Refresh file tree"
				disabled={treeLoading}
			>
				&#x21bb;
			</button>
		</header>

		{#if treeLoading}
			<div class="loading-state">
				<p>Loading worktree...</p>
			</div>
		{:else if treeError}
			<div class="error-state" role="alert">
				<p>{treeError}</p>
				<button class="retry-btn" onclick={() => selectTask(selectedTaskId!)}>Retry</button>
			</div>
		{:else}
			<div class="workspace-content">
				<!-- File tree -->
				<div
					class="file-tree"
					role="tree"
					aria-label="Worktree file tree"
					data-testid="workspace-tree"
				>
					{#if tree.length === 0}
						<div class="tree-empty">
							<p>No files found in this worktree.</p>
						</div>
					{:else}
						{#snippet renderTree(entries: WorkspaceEntry[], depth: number)}
							{#each entries as entry (entry.path)}
								<button
									class="tree-item"
									role="treeitem"
									aria-level={depth + 1}
									aria-label="{entry.is_dir ? 'Directory' : 'File'}: {entry.name}"
									aria-expanded={entry.is_dir ? expanded.has(entry.path) : undefined}
									aria-selected={selectedPath === entry.path}
									class:selected={selectedPath === entry.path}
									class:directory={entry.is_dir}
									style="padding-left: {12 + depth * 16}px"
									onclick={() => selectFile(entry)}
									onkeydown={(e) => handleTreeKeyDown(e, entry)}
									data-testid="tree-item-{entry.path}"
								>
									<span class="tree-icon">{fileIcon(entry)}</span>
									<span class="tree-name">{entry.name}</span>
									{#if !entry.is_dir && entry.size}
										<span class="tree-size">{formatSize(entry.size)}</span>
									{/if}
								</button>
								{#if entry.is_dir && expanded.has(entry.path) && entry.children}
									{@render renderTree(entry.children, depth + 1)}
								{/if}
							{/each}
						{/snippet}
						{@render renderTree(tree, 0)}
					{/if}
				</div>

				<!-- File preview -->
				<div class="file-preview" data-testid="workspace-preview">
					{#if !selectedPath}
						<div class="preview-placeholder">
							<p>Select a file to preview</p>
						</div>
					{:else if fileLoading}
						<div class="preview-placeholder">
							<p>Loading...</p>
						</div>
					{:else if fileError}
						<div class="preview-placeholder preview-error" role="alert">
							<p>{fileError}</p>
						</div>
					{:else if fileContent !== null && selectedEntry}
						<header class="preview-header">
							<span class="preview-path">
								{selectedEntry.path}
								<button
									class="copy-btn copy-btn-inline"
									onclick={() => copyToClipboard(selectedEntry!.path, true)}
									aria-label="Copy file path"
								>
									{copiedPath ? 'Copied' : 'Copy path'}
								</button>
							</span>
						</header>
						<div class="preview-body">
							<button
								class="copy-btn copy-btn-float"
								onclick={() => copyToClipboard(fileContent!)}
								aria-label="Copy file content"
							>
								{copied ? 'Copied' : 'Copy'}
							</button>
							<pre class="preview-content"><code>{fileContent}</code></pre>
						</div>
					{/if}
				</div>
			</div>
		{/if}
	{/if}
</div>

<style>
	.workspace-page {
		height: 100%;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		background: var(--color-bg-primary);
	}

	/* Header */
	.workspace-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.workspace-header h1 {
		margin: 0;
		font-size: 1.125rem;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.header-title {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.file-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		background: var(--color-bg-tertiary);
		padding: 2px 8px;
		border-radius: var(--radius-full);
		flex-shrink: 0;
	}

	.action-btn {
		width: 28px;
		height: 28px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
		font-size: 0.625rem;
		font-weight: 600;
		cursor: pointer;
		transition: border-color 150ms ease;
		text-decoration: none;
		flex-shrink: 0;
	}

	.action-btn:hover:not(:disabled) {
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.action-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* Loading / error / empty states */
	.loading-state,
	.error-state,
	.empty-state {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: var(--space-6);
		color: var(--color-text-muted);
		text-align: center;
	}

	.error-state {
		color: var(--color-text-secondary);
	}

	.empty-icon {
		width: 48px;
		height: 48px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		font-size: 1.5rem;
		font-weight: 700;
		margin-bottom: var(--space-3);
	}

	.empty-state h2 {
		margin: 0 0 var(--space-2);
		font-size: 1rem;
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		max-width: 360px;
		line-height: 1.5;
	}

	.retry-btn {
		margin-top: var(--space-3);
		padding: var(--space-2) var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		transition: border-color 150ms ease;
	}

	.retry-btn:hover {
		border-color: var(--color-accent);
	}

	/* Task list */
	.task-list {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.task-card {
		display: flex;
		align-items: center;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		text-align: left;
		cursor: pointer;
		transition: border-color 150ms ease;
		width: 100%;
		font-family: inherit;
		color: var(--color-text-primary);
	}

	.task-card:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
	}

	.task-card-body {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		overflow: hidden;
	}

	.task-card-title {
		font-size: var(--font-size-sm);
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	.task-card-meta {
		display: flex;
		gap: var(--space-3);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.task-card-branch {
		color: var(--color-accent);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	/* File browser layout */
	.workspace-content {
		flex: 1;
		display: flex;
		overflow: hidden;
	}

	/* File tree panel */
	.file-tree {
		width: 280px;
		min-width: 180px;
		border-right: 1px solid var(--color-border);
		overflow-y: auto;
		padding: var(--space-1) 0;
		flex-shrink: 0;
	}

	.tree-empty {
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.tree-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: 4px 12px;
		border: none;
		background: none;
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		font-family: inherit;
		text-align: left;
		cursor: pointer;
		transition: background-color 100ms ease;
	}

	.tree-item:hover {
		background: var(--color-bg-tertiary);
	}

	.tree-item.selected {
		background: var(--color-accent-muted);
		font-weight: 600;
	}

	.tree-item.directory {
		font-weight: 500;
	}

	.tree-icon {
		width: 16px;
		text-align: center;
		color: var(--color-text-muted);
		font-size: 0.75rem;
		font-weight: 600;
		flex-shrink: 0;
	}

	.tree-name {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.tree-size {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	/* File preview panel */
	.file-preview {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.preview-placeholder {
		display: flex;
		align-items: center;
		justify-content: center;
		flex: 1;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.preview-error {
		color: var(--color-error);
	}

	.preview-header {
		display: flex;
		align-items: center;
		padding: var(--space-2) var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		gap: var(--space-3);
		flex-shrink: 0;
	}

	.preview-path {
		font-size: var(--font-size-sm);
		font-weight: 600;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		flex: 1;
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-primary);
	}

	.preview-body {
		position: relative;
		flex: 1;
		overflow: auto;
	}

	.preview-content {
		margin: 0;
		padding: var(--space-4);
		font-size: var(--font-size-sm);
		line-height: 1.5;
		background: var(--color-bg-primary);
		min-height: 100%;
	}

	.preview-content code {
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		white-space: pre;
	}

	/* Copy buttons */
	.copy-btn {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition:
			border-color 150ms ease,
			color 150ms ease;
	}

	.copy-btn:hover {
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.copy-btn-inline {
		padding: 1px 6px;
		flex-shrink: 0;
	}

	.copy-btn-float {
		position: absolute;
		top: var(--space-3);
		right: var(--space-3);
		padding: var(--space-1) var(--space-3);
		z-index: 1;
	}
</style>
