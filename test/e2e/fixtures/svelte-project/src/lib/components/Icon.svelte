<script lang="ts">
	interface Props {
		name: string;
		size?: number;
	}

	let { name, size = 16 }: Props = $props();

	const sizeStyle = $derived(`width: ${size}px; height: ${size}px;`);
</script>

<span class="icon" style={sizeStyle}>
	{name}
</span>

<style>
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}
</style>
