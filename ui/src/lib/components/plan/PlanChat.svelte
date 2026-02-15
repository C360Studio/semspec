<script lang="ts">
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import ChatDropZone from '$lib/components/chat/ChatDropZone.svelte';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { onMount } from 'svelte';

	interface Props {
		planSlug: string;
	}

	let { planSlug }: Props = $props();

	// Get projectId from plan
	const projectId = $derived.by(() => {
		const plan = plansStore.getBySlug(planSlug);
		return plan?.projectId ?? 'default';
	});

	// Get plan's loop IDs for filtering questions
	const planLoopIds = $derived.by(() => {
		const plan = plansStore.getBySlug(planSlug);
		return plan?.active_loops.map((l) => l.loop_id) ?? [];
	});

	// Filter questions to this plan's loops
	const planQuestions = $derived(
		questionsStore.pending.filter(
			(q) => q.blocked_loop_id && planLoopIds.includes(q.blocked_loop_id)
		)
	);

	// Fetch questions on mount
	onMount(() => {
		questionsStore.fetch('pending');
		const interval = setInterval(() => questionsStore.fetch('pending'), 10000);
		return () => clearInterval(interval);
	});
</script>

<div class="plan-chat">
	{#if planQuestions.length > 0}
		<div class="questions-section">
			<QuestionQueue questions={planQuestions} />
		</div>
	{/if}

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
		gap: var(--space-4);
	}

	.questions-section {
		flex-shrink: 0;
	}

	.chat-section {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}
</style>
