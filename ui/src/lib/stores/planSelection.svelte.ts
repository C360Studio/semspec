/**
 * Plan Selection Store
 *
 * Manages selection state for the plan nav tree, tracking which
 * plan/phase/task is currently selected and providing derived
 * context for chat messages.
 */

export type SelectionType = 'plan' | 'phase' | 'task';

export interface PlanSelection {
	type: SelectionType;
	planSlug: string;
	phaseId?: string;
	taskId?: string;
}

export interface ChatContext {
	type: SelectionType;
	planSlug: string;
	phaseId?: string;
	taskId?: string;
	label: string;
}

class PlanSelectionStore {
	selection = $state<PlanSelection | null>(null);

	// Cache for labels to avoid recomputation
	private labelCache = $state<Map<string, string>>(new Map());

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
		return selection.planSlug;
	}

	/**
	 * Derive chat context from current selection.
	 * Returns context object to attach to messages.
	 */
	get chatContext(): ChatContext | null {
		if (!this.selection) return null;

		return {
			type: this.selection.type,
			planSlug: this.selection.planSlug,
			phaseId: this.selection.phaseId,
			taskId: this.selection.taskId,
			label: this.getLabel(this.selection)
		};
	}

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
