<script lang="ts">
	import BoardView from '$lib/components/board/BoardView.svelte';
	import type { PageData } from './$types';
	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const plans = $derived(data.plans ?? []);
	const activePlans = $derived(
		plans.filter((p) => !['complete', 'failed', 'archived'].includes(p.stage))
	);
</script>

<svelte:head>
	<title>Semspec</title>
</svelte:head>

<BoardView
	plans={activePlans}
	loops={data.loops ?? []}
	tasksByPlan={data.tasksByPlan ?? {}}
/>
