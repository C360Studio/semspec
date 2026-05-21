<script lang="ts">
	/**
	 * Playwright harness for InProgressPanel — the prominent in-progress card
	 * surfaced during long LLM phases (drafting, reviewing, generating_*).
	 *
	 * Scenarios:
	 *   drafting             — title + detail + spinner + elapsed time
	 *   no-elapsed           — startedAt null; elapsed widget should not render
	 *   review               — different copy variant for plan-reviewer phase
	 */
	import InProgressPanel from '$lib/components/plan/InProgressPanel.svelte';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	// Fixed past timestamp so the elapsed counter renders a stable value the
	// test can assert against without racing the clock.
	const tenSecondsAgo = new Date(Date.now() - 10_000).toISOString();
</script>

<div class="harness" data-testid="in-progress-harness">
	{#if data.scenario === 'no-elapsed'}
		<InProgressPanel
			title="Drafting plan…"
			detail="Planner is composing the plan goal, context, and scope…"
			startedAt={null}
		/>
	{:else if data.scenario === 'review'}
		<InProgressPanel
			title="Reviewing plan draft…"
			detail="Plan reviewer is evaluating the draft against SOPs…"
			startedAt={tenSecondsAgo}
		/>
	{:else}
		<InProgressPanel
			title="Drafting plan…"
			detail="Planner is composing the plan goal, context, and scope…"
			startedAt={tenSecondsAgo}
		/>
	{/if}
</div>

<style>
	.harness {
		padding: var(--space-4);
		max-width: 720px;
	}
</style>
