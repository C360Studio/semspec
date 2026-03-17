/**
 * Store for kanban board state.
 * Manages view mode, column visibility, plan filtering, card selection,
 * and requirement/scenario data for the proper Plan → Requirement → Scenario → Task hierarchy.
 */
import { api } from '$lib/api/client';
import type { Requirement } from '$lib/types/requirement';
import type { Scenario } from '$lib/types/scenario';
import { DEFAULT_ACTIVE_STATUSES, type BoardViewMode, type KanbanStatus } from '$lib/types/kanban';

class KanbanStore {
	viewMode = $state<BoardViewMode>('grid');
	selectedPlanSlug = $state<string | null>(null);
	activeStatuses = $state<Set<KanbanStatus>>(new Set(DEFAULT_ACTIVE_STATUSES));
	selectedCardId = $state<string | null>(null);

	// Requirement/scenario caches keyed by plan slug
	requirementsByPlan = $state<Record<string, Requirement[]>>({});
	scenariosByPlan = $state<Record<string, Scenario[]>>({});
	private _fetchingReqs = new Set<string>();
	private _fetchingScenarios = new Set<string>();

	constructor() {
		if (typeof localStorage !== 'undefined') {
			const stored = localStorage.getItem('semspec-board-view-mode');
			if (stored === 'kanban') this.viewMode = 'kanban';
		}
	}

	setViewMode(mode: BoardViewMode) {
		this.viewMode = mode;
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem('semspec-board-view-mode', mode);
		}
	}

	toggleStatus(status: KanbanStatus) {
		const next = new Set(this.activeStatuses);
		if (next.has(status)) {
			if (next.size > 1) next.delete(status);
		} else {
			next.add(status);
		}
		this.activeStatuses = next;
	}

	selectCard(id: string | null) {
		this.selectedCardId = id;
	}

	filterByPlan(slug: string | null) {
		this.selectedPlanSlug = slug;
	}

	/**
	 * Fetch requirements for a plan (cached, deduped).
	 */
	async fetchRequirements(slug: string): Promise<void> {
		if (this.requirementsByPlan[slug] || this._fetchingReqs.has(slug)) return;
		this._fetchingReqs.add(slug);
		try {
			const reqs = await api.requirements.list(slug);
			this.requirementsByPlan[slug] = Array.isArray(reqs) ? reqs : [];
		} catch {
			this.requirementsByPlan[slug] = [];
		} finally {
			this._fetchingReqs.delete(slug);
		}
	}

	/**
	 * Fetch all scenarios for a plan (cached, deduped).
	 */
	async fetchScenarios(slug: string): Promise<void> {
		if (this.scenariosByPlan[slug] || this._fetchingScenarios.has(slug)) return;
		this._fetchingScenarios.add(slug);
		try {
			const scenarios = await api.scenarios.list(slug);
			this.scenariosByPlan[slug] = Array.isArray(scenarios) ? scenarios : [];
		} catch {
			this.scenariosByPlan[slug] = [];
		} finally {
			this._fetchingScenarios.delete(slug);
		}
	}

	/**
	 * Get a requirement title by ID across all cached plans.
	 */
	getRequirementTitle(reqId: string): string | undefined {
		for (const reqs of Object.values(this.requirementsByPlan)) {
			const req = reqs.find((r) => r.id === reqId);
			if (req) return req.title;
		}
		return undefined;
	}

	/**
	 * Get scenario count for a requirement across all cached plans.
	 */
	getScenarioCountForRequirement(reqId: string): number {
		let count = 0;
		for (const scenarios of Object.values(this.scenariosByPlan)) {
			count += scenarios.filter((s) => s.requirement_id === reqId).length;
		}
		return count;
	}
}

export const kanbanStore = new KanbanStore();
