<script lang="ts">
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';

	interface Props {
		planSlug: string;
		projectId?: string;
	}

	// projectId remains in the prop set for parent compatibility but is no longer
	// consumed here — the file-drop affordance went away when /api/sources was
	// deleted (WS-26). semsource auto-indexes the workspace, so doc ingest no
	// longer needs a UI route.
	let { planSlug }: Props = $props();
</script>

<div class="plan-chat">
	<div class="chat-section">
		<ChatPanel title="Plan Chat" {planSlug} />
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
