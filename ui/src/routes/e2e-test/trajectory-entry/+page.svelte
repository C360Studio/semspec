<script lang="ts">
	/**
	 * Playwright harness for TrajectoryEntryCard. Renders one card with a
	 * hard-coded fixture so the bug #7.10 truth-test can click "expand" and
	 * verify that prompt / tool-args / tool-result reveal correctly,
	 * regardless of the `compact` prop. Mirrors the other /e2e-test/* pages.
	 */
	import TrajectoryEntryCard from '$lib/components/trajectory/TrajectoryEntryCard.svelte';
	import type { TrajectoryEntry } from '$lib/types/trajectory';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	// Cast through `unknown` because TrajectoryEntry has many required fields
	// (timestamp, etc.) that the card never reads; the fixture only sets what
	// the template touches. Harness code only — prod call sites go through
	// the API which provides full payloads.
	const toolCallEntry = {
		step_type: 'tool_call',
		tool_name: 'bash',
		tool_arguments: { command: 'go test ./...', timeout_ms: 30000 },
		tool_result: 'PASS\nok\tgithub.com/example/pkg\t0.234s\n',
		tokens_in: 120,
		duration: 234,
		timestamp: '2026-04-23T12:00:00Z'
	} as unknown as TrajectoryEntry;

	const modelCallEntry = {
		step_type: 'model_call',
		model: 'claude-sonnet-4-6',
		provider: 'anthropic',
		response:
			'I will run the tests now to confirm the new endpoint behaves correctly before moving on.',
		tokens_in: 1420,
		tokens_out: 38,
		utilization: 0.42,
		duration: 1890,
		timestamp: '2026-04-23T12:00:01Z'
	} as unknown as TrajectoryEntry;

	// Tool call with no arguments and no result yet — covers the boundary
	// where `hasPreview` is false so the expand button must NOT render.
	const noPreviewEntry = {
		step_type: 'tool_call',
		tool_name: 'bash',
		tokens_in: 0,
		duration: 0,
		timestamp: '2026-04-23T12:00:02Z'
	} as unknown as TrajectoryEntry;

	const entry = $derived(
		data.scenario === 'model-call'
			? modelCallEntry
			: data.scenario === 'no-preview'
				? noPreviewEntry
				: toolCallEntry
	);
</script>

<div class="harness" data-testid="trajectory-harness">
	<!-- compact=true matches how ExecutionTimeline renders entries inside
	     expanded loops. Before the fix, the expand button was hidden in this
	     mode; the truth-test asserts it's now clickable and reveals content. -->
	<TrajectoryEntryCard {entry} compact />
</div>

<style>
	.harness {
		padding: var(--space-4);
		max-width: 640px;
	}
</style>
