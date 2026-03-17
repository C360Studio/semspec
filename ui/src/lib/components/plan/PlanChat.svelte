<script lang="ts">
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import ChatDropZone from '$lib/components/chat/ChatDropZone.svelte';

	interface Props {
		planSlug: string;
		projectId?: string;
	}

	let { planSlug, projectId = 'default' }: Props = $props();
</script>

<div class="plan-chat">
	<div class="chat-section">
		<ChatDropZone {projectId}>
			<ChatPanel title="Plan Chat" {planSlug} />
		</ChatDropZone>
	</div>
</div>

<style>
	.plan-chat {
		display: flex;
		flex-direction: column;
		height: 100%;
	}

	.chat-section {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}
</style>
