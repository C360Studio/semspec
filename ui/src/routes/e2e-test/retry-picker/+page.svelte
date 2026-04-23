<script lang="ts">
	/**
	 * Playwright harness for RetrySelectedPicker. Renders the picker against
	 * stubbed /branches + /retry endpoints so the bug #10-item-1 truth-test
	 * can verify checkbox selection, "retry selected" behaviour, and the
	 * empty / all-selected states without a live backend.
	 */
	import RetrySelectedPicker from '$lib/components/plan/RetrySelectedPicker.svelte';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	let lastRetriedAt = $state<string | null>(null);

	function handleRetried() {
		lastRetriedAt = new Date().toISOString();
	}
</script>

<div class="harness" data-testid="retry-picker-harness">
	<RetrySelectedPicker slug={data.slug} onRetried={handleRetried} />
	{#if lastRetriedAt}
		<div class="last-retried" data-testid="last-retried-at">{lastRetriedAt}</div>
	{/if}
</div>

<style>
	.harness {
		padding: var(--space-4);
		max-width: 640px;
	}
	.last-retried {
		margin-top: var(--space-4);
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
