<script lang="ts">
	import BoardView from '$lib/components/board/BoardView.svelte';
	import type { PageData } from './$types';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const plans = $derived((data.plans ?? []) as PlanWithStatus[]);
	const activePlans = $derived(
		plans.filter((p) => !['complete', 'failed', 'archived'].includes(p.stage))
	);
	const activeLoopCount = $derived(
		(data.loops ?? []).filter((l) => ['pending', 'executing', 'paused'].includes(l.state)).length
	);
	const systemHealthy = $derived(data.system?.healthy ?? false);
</script>

<svelte:head>
	<title>Board - Semspec</title>
</svelte:head>

<BoardView
	plans={activePlans}
	loops={data.loops ?? []}
	tasksByPlan={data.tasksByPlan ?? {}}
	{activeLoopCount}
	{systemHealthy}
/>
