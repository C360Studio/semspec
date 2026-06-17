<script lang="ts">
	/**
	 * Playwright harness for ActivityFeed autoscroll + new-events pill.
	 *
	 * Drives the singleton feedStore directly because ActivityFeed (scope=
	 * "plan") reads from it. Two buttons let the spec inject events
	 * synchronously:
	 *   #seed-N: pre-fill with N synthetic events
	 *   #append-one: append one more event (simulates a fresh SSE arrival)
	 *
	 * The container is given a fixed viewport so the test can deterministically
	 * scroll the events list without relying on full-page layout.
	 */
	import ActivityFeed from '$lib/components/activity/ActivityFeed.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import type { FeedEvent } from '$lib/types/feed';

	let nextId = $state(0);

	function makeEvent(): FeedEvent {
		nextId += 1;
		return {
			id: `synthetic-${nextId}`,
			timestamp: new Date().toISOString(),
			source: 'execution',
			type: 'task_updated',
			kind: 'execution_task',
			summary: `Synthetic event #${nextId}`,
			slug: 'harness'
		};
	}

	function seed(count: number) {
		const events: FeedEvent[] = [];
		for (let i = 0; i < count; i += 1) events.push(makeEvent());
		feedStore.events = events;
		// Pretend the feed is connected so the empty-state copy doesn't render.
		feedStore.connected = true;
		feedStore.currentSlug = 'harness';
	}

	function append() {
		feedStore.events = [...feedStore.events, makeEvent()];
	}
</script>

<div class="harness" data-testid="activity-feed-harness">
	<div class="feed-pane">
		<ActivityFeed maxEvents={100} scope="plan" />
	</div>
	<div class="harness-controls">
		<button type="button" data-testid="seed-30" onclick={() => seed(30)}>Seed 30</button>
		<button type="button" data-testid="append-one" onclick={append}>Append one</button>
	</div>
</div>

<style>
	.harness {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		height: 100vh;
	}

	/* Fixed viewport for the feed so scrollHeight > clientHeight is
	 * predictable across machines. Without this the events-list height
	 * floats with the page and the threshold math becomes flaky. */
	.feed-pane {
		flex: 1;
		min-height: 0;
		max-height: 400px;
		overflow: hidden;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.harness-controls {
		display: flex;
		gap: var(--space-2);
	}

	.harness-controls button {
		padding: var(--space-1) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		cursor: pointer;
	}
</style>
