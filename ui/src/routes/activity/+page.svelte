<script lang="ts">
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';

	const activeLoops = $derived(loopsStore.active);
	const recentEvents = $derived(activityStore.recent.slice(0, 10));
</script>

<svelte:head>
	<title>Activity - Semspec</title>
</svelte:head>

<div class="activity-view">
	<div class="activity-feed">
		<h2 class="section-title">Activity Feed</h2>

		{#if recentEvents.length === 0}
			<div class="empty-feed">
				<Icon name="activity" size={32} />
				<p>No recent activity</p>
			</div>
		{:else}
			<div class="events-list">
				{#each recentEvents as event}
					<div class="event-item">
						<div class="event-icon">
							<Icon
								name={event.type === 'loop_created'
									? 'play'
									: event.type === 'loop_deleted'
										? 'check'
										: 'activity'}
								size={14}
							/>
						</div>
						<div class="event-content">
							<span class="event-type">{event.type.replace('_', ' ')}</span>
							{#if event.loop_id}
								<span class="event-loop">Loop {event.loop_id.slice(-6)}</span>
							{/if}
						</div>
						<span class="event-time">
							{new Date(event.timestamp).toLocaleTimeString()}
						</span>
					</div>
				{/each}
			</div>
		{/if}

		{#if activeLoops.length > 0}
			<div class="active-loops-section">
				<h3 class="subsection-title">Active Loops ({activeLoops.length})</h3>
				<div class="loops-list">
					{#each activeLoops as loop}
						<div class="loop-item">
							<div class="loop-state" data-state={loop.state}></div>
							<span class="loop-id">{loop.loop_id.slice(-8)}</span>
							<span class="loop-progress">{loop.iterations}/{loop.max_iterations}</span>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	<div class="chat-panel">
		<h2 class="section-title">Chat / Commands</h2>
		<div class="chat-content">
			<MessageList messages={messagesStore.messages} />
			<MessageInput
				onSend={(content) => messagesStore.send(content)}
				disabled={messagesStore.sending}
			/>
		</div>
	</div>
</div>

<style>
	.activity-view {
		display: grid;
		grid-template-columns: 1fr 1fr;
		height: 100%;
		gap: 1px;
		background: var(--color-border);
	}

	.activity-feed,
	.chat-panel {
		background: var(--color-bg-primary);
		padding: var(--space-4);
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.section-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-bottom: var(--space-4);
		padding-bottom: var(--space-2);
		border-bottom: 1px solid var(--color-border);
	}

	.subsection-title {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-muted);
		margin: var(--space-4) 0 var(--space-2);
	}

	.empty-feed {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		color: var(--color-text-muted);
		gap: var(--space-2);
	}

	.events-list {
		flex: 1;
		overflow-y: auto;
	}

	.event-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		border-bottom: 1px solid var(--color-border);
		font-size: var(--font-size-sm);
	}

	.event-icon {
		width: 24px;
		height: 24px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		color: var(--color-text-muted);
	}

	.event-content {
		flex: 1;
		display: flex;
		gap: var(--space-2);
	}

	.event-type {
		color: var(--color-text-primary);
		text-transform: capitalize;
	}

	.event-loop {
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.event-time {
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.active-loops-section {
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-3);
		margin-top: var(--space-3);
	}

	.loops-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.loop-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
	}

	.loop-state {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-full);
		background: var(--color-text-muted);
	}

	.loop-state[data-state='executing'] {
		background: var(--color-accent);
		animation: pulse 1.5s ease-in-out infinite;
	}

	.loop-state[data-state='paused'] {
		background: var(--color-warning);
	}

	.loop-id {
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
	}

	.loop-progress {
		margin-left: auto;
		color: var(--color-text-muted);
	}

	.chat-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.5;
		}
	}
</style>
