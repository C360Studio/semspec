<script lang="ts">
	interface Props {
		label: string;
		disabled?: boolean;
		onclick?: (e: Event) => void;
	}

	let { label, disabled = false, onclick }: Props = $props();

	const isActive = $derived(!disabled);
</script>

<button class="btn" {disabled} {onclick}>
	{label}
</button>

<style>
	.btn {
		padding: 0.5rem 1rem;
		border-radius: 4px;
		cursor: pointer;
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
