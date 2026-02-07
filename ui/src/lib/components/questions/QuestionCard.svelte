<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { Question } from '$lib/types';

	interface Props {
		question: Question;
		onAnswer?: (response: string) => void;
		onViewDetails?: () => void;
	}

	let { question, onAnswer, onViewDetails }: Props = $props();

	// Local state for answer form
	let showAnswerForm = $state(false);
	let answerText = $state('');
	let submitting = $state(false);

	// Derive display values
	const shortId = $derived(question.id.slice(0, 10));
	const isPending = $derived(question.status === 'pending');
	const isAnswered = $derived(question.status === 'answered');
	const isTimeout = $derived(question.status === 'timeout');
	const isBlocking = $derived(question.urgency === 'blocking');
	const isHighUrgency = $derived(question.urgency === 'high');

	// Format relative time
	function formatRelativeTime(dateStr: string): string {
		const date = new Date(dateStr);
		const now = new Date();
		const diffMs = now.getTime() - date.getTime();
		const diffMins = Math.floor(diffMs / 60000);
		const diffHours = Math.floor(diffMins / 60);
		const diffDays = Math.floor(diffHours / 24);

		if (diffMins < 1) return 'just now';
		if (diffMins < 60) return `${diffMins}m ago`;
		if (diffHours < 24) return `${diffHours}h ago`;
		return `${diffDays}d ago`;
	}

	// Get urgency icon
	function getUrgencyIcon(): string {
		switch (question.urgency) {
			case 'blocking': return 'alert-circle';
			case 'high': return 'alert-triangle';
			case 'normal': return 'help-circle';
			case 'low': return 'info';
			default: return 'help-circle';
		}
	}

	// Handle answer submission
	async function handleSubmitAnswer() {
		if (!answerText.trim() || !onAnswer) return;

		submitting = true;
		try {
			await onAnswer(answerText.trim());
			answerText = '';
			showAnswerForm = false;
		} finally {
			submitting = false;
		}
	}
</script>

<div
	class="question-card"
	class:pending={isPending}
	class:answered={isAnswered}
	class:timeout={isTimeout}
	class:blocking={isBlocking}
	class:high-urgency={isHighUrgency}
>
	<div class="question-header">
		<span class="question-id" title={question.id}>{shortId}</span>
		<div class="header-right">
			{#if isBlocking || isHighUrgency}
				<span class="urgency-badge" class:blocking={isBlocking}>
					<Icon name={getUrgencyIcon()} size={12} />
					{question.urgency}
				</span>
			{/if}
			<span
				class="status-badge"
				class:pending={isPending}
				class:answered={isAnswered}
				class:timeout={isTimeout}
			>
				{question.status}
			</span>
		</div>
	</div>

	<div class="question-topic">
		<Icon name="tag" size={12} />
		<span>{question.topic}</span>
	</div>

	<div class="question-text">
		{question.question}
	</div>

	{#if question.from_agent && question.from_agent !== 'unknown'}
		<div class="question-meta">
			<Icon name="user" size={12} />
			<span>From: {question.from_agent}</span>
			<span class="separator">|</span>
			<span>{formatRelativeTime(question.created_at)}</span>
		</div>
	{:else}
		<div class="question-meta">
			<span>{formatRelativeTime(question.created_at)}</span>
		</div>
	{/if}

	{#if isAnswered && question.answer}
		<div class="answer-section">
			<div class="answer-label">
				<Icon name="message-square" size={12} />
				Answer
			</div>
			<div class="answer-text">{question.answer}</div>
			{#if question.answered_by}
				<div class="answer-meta">
					by {question.answered_by}
					{#if question.confidence}
						<span class="separator">|</span>
						<span class="confidence" class:high={question.confidence === 'high'}>
							{question.confidence} confidence
						</span>
					{/if}
				</div>
			{/if}
		</div>
	{/if}

	<div class="question-actions">
		{#if isPending && onAnswer}
			{#if showAnswerForm}
				<div class="answer-form">
					<textarea
						bind:value={answerText}
						placeholder="Type your answer..."
						rows="2"
						disabled={submitting}
					></textarea>
					<div class="form-actions">
						<button
							class="btn-cancel"
							onclick={() => { showAnswerForm = false; answerText = ''; }}
							disabled={submitting}
						>
							Cancel
						</button>
						<button
							class="btn-submit"
							onclick={handleSubmitAnswer}
							disabled={!answerText.trim() || submitting}
						>
							{submitting ? 'Submitting...' : 'Submit Answer'}
						</button>
					</div>
				</div>
			{:else}
				<button class="action-btn answer" onclick={() => showAnswerForm = true}>
					<Icon name="edit-3" size={14} />
					Answer
				</button>
			{/if}
		{/if}
		{#if onViewDetails}
			<button class="action-btn details" onclick={onViewDetails}>
				<Icon name="external-link" size={14} />
				Details
			</button>
		{/if}
	</div>
</div>

<style>
	.question-card {
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.question-card.pending {
		border-left: 3px solid var(--color-warning);
	}

	.question-card.answered {
		border-left: 3px solid var(--color-success);
		opacity: 0.85;
	}

	.question-card.timeout {
		border-left: 3px solid var(--color-error);
		opacity: 0.7;
	}

	.question-card.blocking {
		border-color: var(--color-error);
		background: color-mix(in srgb, var(--color-error) 5%, var(--color-bg-tertiary));
	}

	.question-card.high-urgency {
		border-color: var(--color-warning);
	}

	.question-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.header-right {
		display: flex;
		gap: var(--space-2);
		align-items: center;
	}

	.question-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.status-badge,
	.urgency-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-full);
		display: flex;
		align-items: center;
		gap: 4px;
	}

	.status-badge {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.status-badge.pending {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.status-badge.answered {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.status-badge.timeout {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.urgency-badge {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.urgency-badge.blocking {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.question-topic {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-accent);
	}

	.question-text {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: 1.4;
	}

	.question-meta {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.separator {
		color: var(--color-text-muted);
		opacity: 0.5;
	}

	.answer-section {
		margin-top: var(--space-2);
		padding: var(--space-2);
		background: var(--color-bg-elevated);
		border-radius: var(--radius-md);
	}

	.answer-label {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-success);
		margin-bottom: var(--space-1);
	}

	.answer-text {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.answer-meta {
		margin-top: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.confidence {
		color: var(--color-text-secondary);
	}

	.confidence.high {
		color: var(--color-success);
	}

	.question-actions {
		display: flex;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}

	.action-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
		font-size: var(--font-size-xs);
		transition: all var(--transition-fast);
	}

	.action-btn.answer {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.action-btn.details {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.action-btn:hover {
		filter: brightness(1.2);
	}

	.answer-form {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.answer-form textarea {
		width: 100%;
		padding: var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		resize: vertical;
	}

	.answer-form textarea:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.form-actions {
		display: flex;
		gap: var(--space-2);
		justify-content: flex-end;
	}

	.btn-cancel,
	.btn-submit {
		padding: var(--space-1) var(--space-2);
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
		font-size: var(--font-size-xs);
	}

	.btn-cancel {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.btn-submit {
		background: var(--color-accent);
		color: white;
	}

	.btn-submit:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
