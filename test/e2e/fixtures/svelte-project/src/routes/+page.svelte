<script lang="ts">
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';

	let count = $state(0);
	let items = $state<string[]>([]);

	const doubled = $derived(count * 2);
	const itemCount = $derived(items.length);

	function increment() {
		count += 1;
	}

	function addItem() {
		items = [...items, `Item ${items.length + 1}`];
	}
</script>

<main>
	<h1>Svelte 5 Test App</h1>

	<Card title="Counter" subtitle="Click to increment">
		<p>Count: {count}</p>
		<p>Doubled: {doubled}</p>
		<Button label="Increment" onclick={increment} />
	</Card>

	<Card title="Items" subtitle="Manage your list">
		<ul>
			{#each items as item}
				<li>{item}</li>
			{/each}
		</ul>
		<p>Total items: {itemCount}</p>
		<Button label="Add Item" onclick={addItem} />
	</Card>
</main>

<style>
	main {
		max-width: 800px;
		margin: 0 auto;
		padding: 2rem;
	}
</style>
