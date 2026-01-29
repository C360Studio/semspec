<script lang="ts">
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
</script>

<svelte:head>
	<title>Chat - Semspec</title>
</svelte:head>

<div class="chat-view">
	<div class="chat-main">
		<MessageList messages={messagesStore.messages} />
		<MessageInput onSend={(content) => messagesStore.send(content)} disabled={messagesStore.sending} />
	</div>
</div>

<style>
	.chat-view {
		display: flex;
		height: 100%;
	}

	.chat-main {
		flex: 1;
		display: flex;
		flex-direction: column;
		max-width: var(--chat-max-width);
		margin: 0 auto;
		padding: var(--space-4);
		width: 100%;
	}
</style>
