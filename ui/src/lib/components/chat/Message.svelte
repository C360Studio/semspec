<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import { formatTime } from '$lib/utils/format';
	import type { Message } from '$lib/types';

	interface Props {
		message: Message;
	}

	let { message }: Props = $props();

	const typeConfig = {
		user: { icon: 'user', label: 'You' },
		assistant: { icon: 'bot', label: 'Assistant' },
		status: { icon: 'activity', label: 'Status' },
		error: { icon: 'alert-circle', label: 'Error' }
	};

	const config = $derived(typeConfig[message.type] || typeConfig.assistant);
</script>

<div class="message" class:user={message.type === 'user'} class:error={message.type === 'error'}>
	<div class="message-avatar">
		<Icon name={config.icon} size={18} />
	</div>

	<div class="message-content">
		<div class="message-header">
			<span class="message-author">{config.label}</span>
			<span class="message-time">{formatTime(message.timestamp)}</span>
			{#if message.loopId}
				<span class="loop-ref">
					loop:{message.loopId.slice(0, 8)}
				</span>
			{/if}
		</div>

		<div class="message-body">
			{message.content}
		</div>
	</div>
</div>

<style>
	.message {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-3);
		border-radius: var(--radius-lg);
		transition: background var(--transition-fast);
	}

	.message:hover {
		background: var(--color-bg-tertiary);
	}

	.message.user {
		background: var(--color-bg-secondary);
	}

	.message.error {
		background: var(--color-error-muted);
	}

	.message-avatar {
		width: 32px;
		height: 32px;
		border-radius: var(--radius-full);
		background: var(--color-bg-tertiary);
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		color: var(--color-text-secondary);
	}

	.message.user .message-avatar {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.message.error .message-avatar {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.message-content {
		flex: 1;
		min-width: 0;
	}

	.message-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-1);
	}

	.message-author {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.message-time {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.loop-ref {
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-info);
		background: var(--color-info-muted);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
	}

	.message-body {
		font-size: var(--font-size-base);
		line-height: var(--line-height-relaxed);
		color: var(--color-text-primary);
		white-space: pre-wrap;
		word-break: break-word;
	}

	.message.error .message-body {
		color: var(--color-error);
	}
</style>
