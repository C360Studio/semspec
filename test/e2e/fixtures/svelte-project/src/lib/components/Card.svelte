<script lang="ts">
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';
	import { onMount } from 'svelte';

	interface Props {
		title: string;
		subtitle?: string;
		onAction?: () => Promise<void>;
	}

	let { title, subtitle = '', onAction }: Props = $props();

	let isLoading = $state(false);
	let hasError = $state(false);

	const displayTitle = $derived(title.toUpperCase());
	const canSubmit = $derived(!isLoading && !hasError);

	async function handleClick() {
		if (!onAction) return;

		isLoading = true;
		hasError = false;

		try {
			await onAction();
		} catch (err) {
			hasError = true;
		} finally {
			isLoading = false;
		}
	}

	$effect(() => {
		console.log('Card title changed:', title);
	});
</script>

<div class="card">
	<div class="header">
		<Icon name="card" />
		<h2>{displayTitle}</h2>
		{#if subtitle}
			<p class="subtitle">{subtitle}</p>
		{/if}
	</div>

	<div class="content">
		<slot />
	</div>

	{#if onAction}
		<div class="footer">
			<Button label={isLoading ? 'Loading...' : 'Submit'} disabled={!canSubmit} onclick={handleClick} />
		</div>
	{/if}
</div>

<style>
	.card {
		border: 1px solid #ccc;
		border-radius: 8px;
		padding: 1rem;
	}

	.header {
		margin-bottom: 1rem;
	}

	.subtitle {
		color: #666;
		font-size: 0.875rem;
	}
</style>
