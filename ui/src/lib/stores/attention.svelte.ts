import { changesStore } from './changes.svelte';
import { loopsStore } from './loops.svelte';
import { questionsStore } from './questions.svelte';
import type { AttentionItem, AttentionType } from '$lib/types/changes';

/**
 * Store for items requiring human attention.
 * Derives attention items from changes, loops, and questions stores.
 *
 * Attention sources:
 * - Changes with status 'reviewed' → approval_needed
 * - Questions with status 'pending' → question_pending
 * - Loops in 'failed' state → task_failed
 * - (Future) Tasks with status 'blocked' → task_blocked
 */
class AttentionStore {
	/**
	 * All attention items derived from other stores
	 */
	get items(): AttentionItem[] {
		const items: AttentionItem[] = [];

		// Changes needing approval (status === 'reviewed')
		for (const change of changesStore.needingApproval) {
			items.push({
				type: 'approval_needed',
				change_slug: change.slug,
				title: `Approve spec for "${change.slug}"`,
				description: 'The spec is ready for review and approval to generate tasks.',
				action_url: `/changes/${change.slug}`,
				created_at: change.updated_at
			});
		}

		// Pending questions
		for (const question of questionsStore.pending) {
			items.push({
				type: 'question_pending',
				loop_id: question.blocked_loop_id,
				title: `Answer question from ${question.from_agent}`,
				description: question.question,
				action_url: '/activity',
				created_at: question.created_at
			});
		}

		// Failed loops
		for (const loop of loopsStore.all.filter((l) => l.state === 'failed')) {
			// Try to find the change slug from active changes
			const change = changesStore.withActiveLoops.find((c) =>
				c.active_loops.some((al) => al.loop_id === loop.loop_id)
			);

			items.push({
				type: 'task_failed',
				loop_id: loop.loop_id,
				change_slug: change?.slug,
				title: `Task failed in loop ${loop.loop_id.slice(-6)}`,
				description: `Loop failed after ${loop.iterations} iterations`,
				action_url: change ? `/changes/${change.slug}` : '/activity',
				created_at: loop.created_at || new Date().toISOString()
			});
		}

		// Sort by created_at descending (newest first)
		return items.sort(
			(a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
		);
	}

	/**
	 * Count of attention items (for badge display)
	 */
	get count(): number {
		return this.items.length;
	}

	/**
	 * Items grouped by type
	 */
	get byType(): Record<AttentionType, AttentionItem[]> {
		const grouped: Record<AttentionType, AttentionItem[]> = {
			approval_needed: [],
			question_pending: [],
			task_failed: [],
			task_blocked: []
		};

		for (const item of this.items) {
			grouped[item.type].push(item);
		}

		return grouped;
	}

	/**
	 * Check if there are any items of a specific type
	 */
	hasType(type: AttentionType): boolean {
		return this.items.some((i) => i.type === type);
	}

	/**
	 * Get items for a specific change
	 */
	forChange(slug: string): AttentionItem[] {
		return this.items.filter((i) => i.change_slug === slug);
	}
}

export const attentionStore = new AttentionStore();
