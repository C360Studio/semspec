<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { onMount } from 'svelte';

	let answeringId = $state<string | null>(null);
	let answerText = $state('');
	let submitting = $state(false);

	const pendingQuestions = $derived(questionsStore.pending);
	const blockingCount = $derived(questionsStore.blocking.length);

	onMount(() => {
		questionsStore.fetch('pending');
		const interval = setInterval(() => questionsStore.fetch('pending'), 10000);
		return () => clearInterval(interval);
	});

	async function submitAnswer(questionId: string) {
		if (!answerText.trim() || submitting) return;

		submitting = true;
		try {
			await questionsStore.answer(questionId, answerText.trim());
			answerText = '';
			answeringId = null;
		} finally {
			submitting = false;
		}
	}

	function startAnswering(id: string) {
		answeringId = id;
		answerText = '';
	}

	function cancelAnswering() {
		answeringId = null;
		answerText = '';
	}

	function getUrgencyColor(urgency: string): string {
		switch (urgency) {
			case 'blocking':
				return 'var(--color-error)';
			case 'high':
				return 'var(--color-warning)';
			default:
				return 'var(--color-text-muted)';
		}
	}
</script>

{#if pendingQuestions.length > 0}
	<div class="question-queue">
		<div class="queue-header">
			<Icon name="help-circle" size={16} />
			<span class="queue-title">Questions</span>
			<span class="queue-count">{pendingQuestions.length}</span>
			{#if blockingCount > 0}
				<span class="blocking-badge">
					<Icon name="alert-circle" size={12} />
					{blockingCount} blocking
				</span>
			{/if}
		</div>

		<div class="questions-list">
			{#each pendingQuestions as question (question.id)}
				<div class="question-item" data-urgency={question.urgency}>
					<div class="question-header">
						<span class="question-from" style="color: {getUrgencyColor(question.urgency)}">
							{question.from_agent}
						</span>
						<span class="question-topic">{question.topic}</span>
						{#if question.urgency === 'blocking'}
							<span class="urgency-tag blocking">blocking</span>
						{:else if question.urgency === 'high'}
							<span class="urgency-tag high">high</span>
						{/if}
					</div>

					<p class="question-text">{question.question}</p>

					{#if answeringId === question.id}
						<div class="answer-form">
							<textarea
								bind:value={answerText}
								placeholder="Type your answer..."
								rows="2"
								disabled={submitting}
							></textarea>
							<div class="answer-actions">
								<button
									class="btn-cancel"
									onclick={cancelAnswering}
									disabled={submitting}
								>
									Cancel
								</button>
								<button
									class="btn-submit"
									onclick={() => submitAnswer(question.id)}
									disabled={!answerText.trim() || submitting}
								>
									{submitting ? 'Sending...' : 'Send Answer'}
								</button>
							</div>
						</div>
					{:else}
						<div class="question-actions">
							<button
								class="answer-btn"
								onclick={() => startAnswering(question.id)}
							>
								<Icon name="send" size={12} />
								Answer
							</button>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	</div>
{/if}

<style>
	.question-queue {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.queue-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
	}

	.queue-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.queue-count {
		background: var(--color-warning-muted);
		color: var(--color-warning);
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
	}

	.blocking-badge {
		display: flex;
		align-items: center;
		gap: 2px;
		margin-left: auto;
		padding: 2px 6px;
		background: var(--color-error-muted);
		color: var(--color-error);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
	}

	.questions-list {
		display: flex;
		flex-direction: column;
	}

	.question-item {
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.question-item:last-child {
		border-bottom: none;
	}

	.question-item[data-urgency='blocking'] {
		background: rgba(239, 68, 68, 0.05);
	}

	.question-item[data-urgency='high'] {
		background: rgba(245, 158, 11, 0.05);
	}

	.question-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-2);
	}

	.question-from {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
	}

	.question-topic {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		padding: 1px 4px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
	}

	.urgency-tag {
		margin-left: auto;
		font-size: 10px;
		padding: 1px 4px;
		border-radius: var(--radius-sm);
		font-weight: var(--font-weight-medium);
		text-transform: uppercase;
	}

	.urgency-tag.blocking {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.urgency-tag.high {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.question-text {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		margin: 0 0 var(--space-2);
		line-height: var(--line-height-normal);
	}

	.question-actions {
		display: flex;
		gap: var(--space-2);
	}

	.answer-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.answer-btn:hover {
		background: var(--color-accent);
		color: white;
	}

	.answer-form {
		margin-top: var(--space-2);
	}

	.answer-form textarea {
		width: 100%;
		padding: var(--space-2);
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		font-family: inherit;
		resize: none;
	}

	.answer-form textarea:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.answer-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}

	.btn-cancel,
	.btn-submit {
		padding: var(--space-1) var(--space-3);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
		cursor: pointer;
	}

	.btn-cancel {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	.btn-cancel:hover {
		background: var(--color-bg-elevated);
	}

	.btn-submit {
		background: var(--color-accent);
		color: white;
	}

	.btn-submit:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.btn-submit:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
