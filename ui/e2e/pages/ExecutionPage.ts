import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the execution components rendered on the test harness
 * at /e2e-test/execution?scenario=<name>.
 *
 * Covers AgentTree, DAGView, LoopDetail, and RetrospectiveView.
 */
export class ExecutionPage {
	readonly page: Page;

	// ---- AgentTree locators ----
	readonly agentTree: Locator;
	readonly treeNodes: Locator;
	readonly treeEmptyState: Locator;

	// ---- DAGView locators ----
	readonly dagView: Locator;
	readonly dagHeader: Locator;
	readonly dagNodes: Locator;
	readonly dagEmptyState: Locator;
	readonly dagSummary: Locator;

	// ---- LoopDetail locators ----
	readonly loopDetailPanel: Locator;
	readonly loopDetailClose: Locator;
	readonly loopDetailBackdrop: Locator;
	readonly metaGrid: Locator;

	// ---- RetrospectiveView locators ----
	readonly retroView: Locator;
	readonly retroStatsBar: Locator;
	readonly retroEmptyState: Locator;
	readonly requirementHeaders: Locator;

	constructor(page: Page) {
		this.page = page;

		// AgentTree
		this.agentTree = page.locator('ul.tree-list[role="tree"]');
		this.treeNodes = page.locator('li.tree-node[role="treeitem"]');
		this.treeEmptyState = page.locator('.harness-agent-tree .empty-state');

		// DAGView
		this.dagView = page.locator('.dag-view');
		this.dagHeader = page.locator('.dag-header');
		this.dagNodes = page.locator('button.dag-node');
		this.dagEmptyState = page.locator('.dag-view .empty-state');
		this.dagSummary = page.locator('.dag-summary');

		// LoopDetail
		this.loopDetailPanel = page.locator('aside.detail-panel');
		this.loopDetailClose = page.locator('aside.detail-panel .close-btn');
		this.loopDetailBackdrop = page.locator('.detail-backdrop');
		this.metaGrid = page.locator('dl.meta-grid');

		// RetrospectiveView
		this.retroView = page.locator('.retro-view');
		this.retroStatsBar = page.locator('.stats-bar');
		this.retroEmptyState = page.locator('.retro-view .empty-state');
		this.requirementHeaders = page.locator('button.requirement-header');
	}

	// ---- Navigation ----

	/**
	 * Navigate to the test harness for a given scenario.
	 * Waits for the harness to finish loading before returning.
	 */
	async goto(scenario: string): Promise<void> {
		await this.page.goto(`/e2e-test/execution?scenario=${scenario}`);
		// Wait until loading completes — the harness sets data-loading="false"
		await this.page
			.locator('[data-loading="false"]')
			.waitFor({ state: 'attached', timeout: 15000 });
	}

	// ---- AgentTree methods ----

	async getTreeNodeCount(): Promise<number> {
		return this.treeNodes.count();
	}

	async getTreeNodeRoles(): Promise<string[]> {
		const nodes = await this.treeNodes.all();
		const roles: string[] = [];
		for (const node of nodes) {
			const role = await node.locator('.node-role').textContent();
			if (role) roles.push(role.trim());
		}
		return roles;
	}

	async clickTreeNode(role: string): Promise<void> {
		// Match by partial role text — role text is formatted (e.g. "Orchestrator")
		const node = this.treeNodes.filter({ hasText: new RegExp(role, 'i') }).first();
		const contentBtn = node.locator('.node-content');
		await contentBtn.click();
	}

	async expandTreeNode(loopId: string): Promise<void> {
		// Find the tree item containing the loopId (truncated to 8 chars in task-id display)
		// .expand-btn.visible is set via Svelte's class:visible={hasChildren} — only present when
		// the node has children
		const node = this.treeNodes
			.filter({ hasText: loopId.slice(0, 8) })
			.filter({ has: this.page.locator('.expand-btn.visible') })
			.first();
		await node.locator('.expand-btn.visible').click();
	}

	async isTreeEmpty(): Promise<boolean> {
		return this.treeEmptyState.isVisible();
	}

	// ---- DAGView methods ----

	async getDAGNodeCount(): Promise<number> {
		return this.dagNodes.count();
	}

	async getDAGStatus(): Promise<string> {
		const badge = this.dagHeader.locator('.status-badge');
		const text = await badge.textContent();
		return text?.trim() ?? '';
	}

	async clickDAGNode(nodeId: string): Promise<void> {
		// DAG nodes show a truncated node ID in .node-id span
		const node = this.dagNodes.filter({ hasText: nodeId.slice(0, 6) }).first();
		await node.click();
	}

	async getDAGSummaryText(): Promise<string> {
		const text = await this.dagSummary.textContent();
		return text?.trim() ?? '';
	}

	async isDAGEmpty(): Promise<boolean> {
		return this.dagEmptyState.isVisible();
	}

	// ---- LoopDetail methods ----

	async isDetailPanelVisible(): Promise<boolean> {
		return this.loopDetailPanel.isVisible();
	}

	async closeDetailPanel(): Promise<void> {
		await this.loopDetailClose.click();
	}

	async getDetailRole(): Promise<string> {
		const title = this.loopDetailPanel.locator('.panel-title');
		const text = await title.textContent();
		return text?.trim() ?? '';
	}

	async getDetailStatus(): Promise<string> {
		const badge = this.loopDetailPanel.locator('.status-badge');
		const text = await badge.textContent();
		return text?.trim() ?? '';
	}

	async getDetailMetaValue(label: string): Promise<string> {
		// Each .meta-item has a <dt> (label) and <dd> (value)
		const item = this.metaGrid
			.locator('.meta-item')
			.filter({ has: this.page.locator('dt', { hasText: label }) });
		const value = await item.locator('dd').textContent();
		return value?.trim() ?? '';
	}

	// ---- RetrospectiveView methods ----

	async getRetroStats(): Promise<{ requirements: string; scenarios: string; tasks: string }> {
		const values = await this.retroStatsBar.locator('.stat-value').allTextContents();
		const labels = await this.retroStatsBar.locator('.stat-label').allTextContents();

		const result = { requirements: '0', scenarios: '0', tasks: '0' };
		for (let i = 0; i < labels.length; i++) {
			const lbl = labels[i].trim().toLowerCase();
			const val = (values[i] ?? '0').trim();
			if (lbl.includes('requirement')) result.requirements = val;
			else if (lbl.includes('scenario')) result.scenarios = val;
			else if (lbl.includes('task')) result.tasks = val;
		}
		return result;
	}

	async expandRequirement(title: string): Promise<void> {
		const header = this.requirementHeaders.filter({ hasText: title });
		// Only click if not already expanded
		const expanded = await header.getAttribute('aria-expanded');
		if (expanded !== 'true') {
			await header.click();
		}
	}

	async getRequirementCount(): Promise<number> {
		return this.requirementHeaders.count();
	}

	async isRetroEmpty(): Promise<boolean> {
		return this.retroEmptyState.isVisible();
	}
}
