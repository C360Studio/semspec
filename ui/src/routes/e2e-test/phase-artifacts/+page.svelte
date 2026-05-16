<script lang="ts">
	/**
	 * Playwright harness for PhaseArtifactsView. Renders the component in
	 * isolation against stubbed /plan-manager/plans/{slug}/artifacts endpoints
	 * so smoke tests can assert TOC + anchor + cross-link behavior without
	 * touching the live LLM pipeline. Not linked from navigation.
	 */
	import PhaseArtifactsView from '$lib/components/plan/PhaseArtifactsView.svelte';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();
</script>

<div class="harness" data-testid="phase-artifacts-harness">
	<PhaseArtifactsView slug={data.slug} />
</div>

<style>
	.harness {
		height: 100vh;
		width: 100vw;
		overflow-y: auto;
		padding: 16px;
	}
</style>
