<script lang="ts">
	/**
	 * Playwright harness for PlanWorkspace. Renders the component in isolation
	 * against stubbed endpoints so truth-tests can assert DOM output for bug
	 * #7.6 (Files view). Not linked from navigation.
	 */
	import { PlanWorkspace } from '$lib/components/workspace';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();
</script>

<div class="harness" data-testid="plan-workspace-harness">
	<PlanWorkspace slug={data.slug} />
</div>

<style>
	.harness {
		height: 100vh;
		width: 100vw;
		overflow: hidden;
	}
</style>
