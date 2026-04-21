<script lang="ts">
	/**
	 * Playwright harness for PlanCard. Renders one card per plan returned by
	 * the stubbed /plan-manager/plans call so truth-tests can assert DOM
	 * against fixtures without racing real backend state.
	 */
	import PlanCard from '$lib/components/board/PlanCard.svelte';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();
</script>

<div class="harness" data-testid="plan-card-harness">
	{#each data.plans as plan (plan.slug)}
		<PlanCard {plan} />
	{/each}
</div>

<style>
	.harness {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-4);
	}
</style>
