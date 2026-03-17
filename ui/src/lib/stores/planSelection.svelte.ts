/**
 * Plan Selection Store
 *
 * Manages selection state for the plan nav tree, tracking which
 * plan/phase/task is currently selected and providing derived
 * context for chat messages.
 */

export type SelectionType = 'plan' | 'phase' | 'task' | 'requirement' | 'scenario';

export interface PlanSelection {
	type: SelectionType;
	planSlug: string;
	phaseId?: string;
	taskId?: string;
	requirementId?: string;
	scenarioId?: string;
}

export interface ChatContext {
	type: SelectionType;
	planSlug: string;
	phaseId?: string;
	taskId?: string;
	requirementId?: string;
	scenarioId?: string;
	label: string;
}

class PlanSelectionStore {
	selection = $state<PlanSelection | null>(null);

	// Plain Map — not reactive since labels are only used for display lookup
	private labelCache = new Map<string, string>();

	selectPlan(slug: string): void {
		this.selection = {
			type: 'plan',
			planSlug: slug
		};
	}

	selectPhase(slug: string, phaseId: string): void {
		this.selection = {
			type: 'phase',
			planSlug: slug,
			phaseId
		};
	}

	selectTask(slug: string, phaseId: string, taskId: string): void {
		this.selection = {
			type: 'task',
			planSlug: slug,
			phaseId,
			taskId
		};
	}

	selectRequirement(slug: string, requirementId: string): void {
		this.selection = {
			type: 'requirement',
			planSlug: slug,
			requirementId
		};
	}

	selectScenario(slug: string, requirementId: string, scenarioId: string): void {
		this.selection = {
			type: 'scenario',
			planSlug: slug,
			requirementId,
			scenarioId
		};
	}

	clear(): void {
		this.selection = null;
	}

	/**
	 * Set a label for a specific item (plan/phase/task).
	 * This is called by components that have access to the actual data.
	 */
	setLabel(key: string, label: string): void {
		this.labelCache.set(key, label);
	}

	/**
	 * Get the label for a selection item.
	 */
	getLabel(selection: PlanSelection): string {
		if (selection.type === 'plan') {
			return this.labelCache.get(`plan:${selection.planSlug}`) ?? selection.planSlug;
		}
		if (selection.type === 'phase' && selection.phaseId) {
			return this.labelCache.get(`phase:${selection.phaseId}`) ?? `Phase ${selection.phaseId.slice(0, 8)}`;
		}
		if (selection.type === 'task' && selection.taskId) {
			return this.labelCache.get(`task:${selection.taskId}`) ?? `Task ${selection.taskId.slice(0, 8)}`;
		}
		if (selection.type === 'requirement' && selection.requirementId) {
			return this.labelCache.get(`requirement:${selection.requirementId}`) ?? `Requirement ${selection.requirementId.slice(0, 8)}`;
		}
		if (selection.type === 'scenario' && selection.scenarioId) {
			return this.labelCache.get(`scenario:${selection.scenarioId}`) ?? `Scenario ${selection.scenarioId.slice(0, 8)}`;
		}
		return selection.planSlug;
	}

	/**
	 * Derive chat context from current selection.
	 * Uses $derived to cache and avoid creating new object references on every read.
	 */
	chatContext = $derived.by((): ChatContext | null => {
		if (!this.selection) return null;

		return {
			type: this.selection.type,
			planSlug: this.selection.planSlug,
			phaseId: this.selection.phaseId,
			taskId: this.selection.taskId,
			requirementId: this.selection.requirementId,
			scenarioId: this.selection.scenarioId,
			label: this.getLabel(this.selection)
		};
	});

	/**
	 * Check if a specific item is selected.
	 */
	isSelected(type: SelectionType, id: string): boolean {
		if (!this.selection) return false;

		switch (type) {
			case 'plan':
				return this.selection.type === 'plan' && this.selection.planSlug === id;
			case 'phase':
				return (
					(this.selection.type === 'phase' || this.selection.type === 'task') &&
					this.selection.phaseId === id
				);
			case 'task':
				return this.selection.type === 'task' && this.selection.taskId === id;
			case 'requirement':
				return (
					(this.selection.type === 'requirement' || this.selection.type === 'scenario') &&
					this.selection.requirementId === id
				);
			case 'scenario':
				return this.selection.type === 'scenario' && this.selection.scenarioId === id;
			default:
				return false;
		}
	}

	/**
	 * Check if a phase should be expanded (contains selected task or is selected).
	 */
	isPhaseExpanded(phaseId: string): boolean {
		if (!this.selection) return false;
		return this.selection.phaseId === phaseId;
	}
}

export const planSelectionStore = new PlanSelectionStore();
