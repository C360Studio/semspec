<script lang="ts">
	import BoardView from '$lib/components/board/BoardView.svelte';
	import type { LayoutData } from './$types';

	interface Props {
		data: LayoutData;
	}

	let { data }: Props = $props();
</script>

<svelte:head>
	<title>Semspec</title>
</svelte:head>

<BoardView plans={data.plans ?? []} loops={data.loops ?? []} />
