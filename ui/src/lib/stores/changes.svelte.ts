import { mockChanges, mockDocuments, mockParsedTasks } from '$lib/api/mock-changes';
import type {
	ChangeWithStatus,
	WorkflowStatus,
	DocumentInfo,
	ParsedTask
} from '$lib/types/changes';

/**
 * Store for workflow changes.
 * Follows the same pattern as loopsStore.
 *
 * Currently uses mock data. Phase 4 will switch to real API:
 * - GET /api/workflow/changes
 * - GET /api/workflow/changes/{slug}
 * - GET /api/workflow/changes/{slug}/documents/{type}
 * - GET /api/workflow/changes/{slug}/tasks
 */
class ChangesStore {
	all = $state<ChangeWithStatus[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);
	selectedSlug = $state<string | null>(null);

	/**
	 * Active changes (not completed or rejected)
	 */
	get active(): ChangeWithStatus[] {
		return this.all.filter(
			(c) => !['complete', 'rejected'].includes(c.status)
		);
	}

	/**
	 * Changes grouped by status
	 */
	get byStatus(): Record<WorkflowStatus, ChangeWithStatus[]> {
		const grouped: Record<WorkflowStatus, ChangeWithStatus[]> = {
			created: [],
			drafted: [],
			reviewed: [],
			approved: [],
			implementing: [],
			complete: [],
			rejected: []
		};

		for (const change of this.all) {
			grouped[change.status].push(change);
		}

		return grouped;
	}

	/**
	 * Get a single change by slug
	 */
	getBySlug(slug: string): ChangeWithStatus | undefined {
		return this.all.find((c) => c.slug === slug);
	}

	/**
	 * Changes that need approval (status === 'reviewed')
	 */
	get needingApproval(): ChangeWithStatus[] {
		return this.all.filter((c) => c.status === 'reviewed');
	}

	/**
	 * Changes with active loops
	 */
	get withActiveLoops(): ChangeWithStatus[] {
		return this.all.filter((c) => c.active_loops.length > 0);
	}

	/**
	 * Fetch all changes
	 */
	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			// TODO: Phase 4 - Replace with real API call
			// this.all = await api.workflow.getChanges();
			await new Promise((resolve) => setTimeout(resolve, 200));
			this.all = mockChanges;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch changes';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Get documents for a specific change
	 */
	async getDocuments(slug: string): Promise<DocumentInfo[]> {
		// TODO: Phase 4 - Replace with real API call
		const change = this.getBySlug(slug);
		if (!change) return [];

		const docs: DocumentInfo[] = [];
		const mockDocs = mockDocuments[slug] || {};

		for (const type of ['proposal', 'design', 'spec', 'tasks'] as const) {
			const fileKey = `has_${type}` as keyof typeof change.files;
			const exists = change.files[fileKey];
			const doc = mockDocs[type];

			docs.push({
				type,
				exists,
				content: doc?.content,
				generated_at: doc?.generated_at,
				model: doc?.model
			});
		}

		return docs;
	}

	/**
	 * Get parsed tasks for a specific change
	 */
	async getTasks(slug: string): Promise<ParsedTask[]> {
		// TODO: Phase 4 - Replace with real API call
		// return api.workflow.getTasks(slug);
		await new Promise((resolve) => setTimeout(resolve, 100));
		return mockParsedTasks[slug as keyof typeof mockParsedTasks] || [];
	}

	/**
	 * Approve a change (transition to approved status)
	 */
	async approve(slug: string): Promise<void> {
		// TODO: Phase 4 - Replace with real API call
		// await api.workflow.approve(slug);
		const change = this.getBySlug(slug);
		if (change) {
			change.status = 'approved';
		}
	}
}

export const changesStore = new ChangesStore();
