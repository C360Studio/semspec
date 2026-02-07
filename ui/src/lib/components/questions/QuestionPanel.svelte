<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import QuestionCard from './QuestionCard.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import type { QuestionStatus } from '$lib/types';

	interface Props {
		collapsed?: boolean;
		onToggle?: () => void;
	}

	let { collapsed = false, onToggle }: Props = $props();

	let filter = $state<QuestionStatus | 'all'>('pending');
	let showAskForm = $state(false);
	let askTopic = $state('');
	let askQuestion = $state('');
	let submittingAsk = $state(false);

	// Initial fetch
	$effect(() => {
		questionsStore.fetch(filter);
	});

	// Refresh when filter changes
	$effect(() => {
		questionsStore.fetch(filter);
	});

	// Get questions based on filter
	const displayQuestions = $derived(() => {
		switch (filter) {
			case 'pending': return questionsStore.pending;
			case 'answered': return questionsStore.answered;
			case 'timeout': return questionsStore.timedOut;
			default: return questionsStore.all;
		}
	});

	// Handle answer submission
	async function handleAnswer(questionId: string, response: string) {
		await questionsStore.answer(questionId, response);
	}

	// Handle ask submission
	async function handleSubmitAsk() {
		if (!askTopic.trim() || !askQuestion.trim()) return;

		submittingAsk = true;
		try {
			await questionsStore.ask(askTopic.trim(), askQuestion.trim());
			askTopic = '';
			askQuestion = '';
			showAskForm = false;
		} finally {
			submittingAsk = false;
		}
	}
</script>

<section class="question-panel" class:collapsed>
	{#if onToggle}
		<button class="panel-toggle" onclick={onToggle} title={collapsed ? 'Expand' : 'Collapse'}>
			<Icon name={collapsed ? 'chevron-left' : 'chevron-right'} size={16} />
		</button>
	{/if}

	{#if !collapsed}
		<div class="panel-header">
			<h2>
				<Icon name="help-circle" size={16} />
				Questions
			</h2>
			<div class="header-actions">
				{#if questionsStore.pending.length > 0}
					<span class="pending-badge" title="Pending questions">
						{questionsStore.pending.length}
					</span>
				{/if}
				<button
					class="ask-btn"
					onclick={() => showAskForm = !showAskForm}
					title="Ask a question"
				>
					<Icon name="plus" size={14} />
				</button>
			</div>
		</div>

		{#if showAskForm}
			<div class="ask-form">
				<div class="form-field">
					<label for="ask-topic">Topic</label>
					<input
						id="ask-topic"
						type="text"
						bind:value={askTopic}
						placeholder="e.g., api.semstreams"
						disabled={submittingAsk}
					/>
				</div>
				<div class="form-field">
					<label for="ask-question">Question</label>
					<textarea
						id="ask-question"
						bind:value={askQuestion}
						placeholder="What do you need to know?"
						rows="2"
						disabled={submittingAsk}
					></textarea>
				</div>
				<div class="form-actions">
					<button
						class="btn-cancel"
						onclick={() => { showAskForm = false; askTopic = ''; askQuestion = ''; }}
						disabled={submittingAsk}
					>
						Cancel
					</button>
					<button
						class="btn-submit"
						onclick={handleSubmitAsk}
						disabled={!askTopic.trim() || !askQuestion.trim() || submittingAsk}
					>
						{submittingAsk ? 'Submitting...' : 'Ask'}
					</button>
				</div>
			</div>
		{/if}

		<div class="filter-tabs">
			<button
				class="tab"
				class:active={filter === 'pending'}
				onclick={() => filter = 'pending'}
			>
				Pending
				{#if questionsStore.pending.length > 0}
					<span class="tab-count">{questionsStore.pending.length}</span>
				{/if}
			</button>
			<button
				class="tab"
				class:active={filter === 'answered'}
				onclick={() => filter = 'answered'}
			>
				Answered
			</button>
			<button
				class="tab"
				class:active={filter === 'all'}
				onclick={() => filter = 'all'}
			>
				All
			</button>
		</div>

		<div class="panel-content">
			{#if questionsStore.loading && questionsStore.all.length === 0}
				<div class="loading-state">
					<Icon name="loader" size={20} />
					<span>Loading questions...</span>
				</div>
			{:else if displayQuestions().length === 0}
				<div class="empty-state">
					<Icon name="inbox" size={24} />
					<span>No {filter === 'all' ? '' : filter} questions</span>
					<p class="empty-hint">Use /ask to create a question</p>
				</div>
			{:else}
				<div class="question-list">
					{#each displayQuestions() as question (question.id)}
						<QuestionCard
							{question}
							onAnswer={(response) => handleAnswer(question.id, response)}
						/>
					{/each}
				</div>
			{/if}

			{#if questionsStore.blocking.length > 0 && filter !== 'pending'}
				<div class="blocking-notice">
					<Icon name="alert-circle" size={14} />
					<span>{questionsStore.blocking.length} blocking question{questionsStore.blocking.length > 1 ? 's' : ''} need attention</span>
				</div>
			{/if}
		</div>

		<div class="panel-footer">
			{#if questionsStore.lastRefresh}
				<span class="refresh-time">
					Updated {questionsStore.lastRefresh.toLocaleTimeString()}
				</span>
			{/if}
			<button class="refresh-btn" onclick={() => questionsStore.fetch(filter)} disabled={questionsStore.loading}>
				<Icon name="refresh-cw" size={12} />
			</button>
		</div>
	{/if}
</section>

<style>
	.question-panel {
		width: 100%;
		height: 100%;
		background: var(--color-bg-secondary);
		display: flex;
		flex-direction: column;
		position: relative;
	}

	.question-panel.collapsed {
		display: none;
	}

	.panel-toggle {
		position: absolute;
		left: -12px;
		top: 50%;
		transform: translateY(-50%);
		width: 24px;
		height: 24px;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		color: var(--color-text-muted);
		z-index: 10;
	}

	.panel-toggle:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.panel-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.panel-header h2 {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.pending-badge {
		background: var(--color-warning-muted);
		color: var(--color-warning);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		padding: 2px 8px;
		border-radius: var(--radius-full);
	}

	.ask-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
	}

	.ask-btn:hover {
		background: var(--color-accent);
		color: white;
	}

	.ask-form {
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
	}

	.form-field {
		margin-bottom: var(--space-2);
	}

	.form-field label {
		display: block;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		margin-bottom: var(--space-1);
	}

	.form-field input,
	.form-field textarea {
		width: 100%;
		padding: var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.form-field input:focus,
	.form-field textarea:focus {
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

	.filter-tabs {
		display: flex;
		border-bottom: 1px solid var(--color-border);
	}

	.tab {
		flex: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		padding: var(--space-2);
		background: transparent;
		border: none;
		border-bottom: 2px solid transparent;
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.tab:hover {
		color: var(--color-text-primary);
	}

	.tab.active {
		color: var(--color-accent);
		border-bottom-color: var(--color-accent);
	}

	.tab-count {
		background: var(--color-warning-muted);
		color: var(--color-warning);
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
	}

	.panel-content {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-3);
	}

	.question-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.loading-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		margin: 0;
	}

	.blocking-notice {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2);
		margin-top: var(--space-3);
		background: var(--color-error-muted);
		color: var(--color-error);
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
	}

	.panel-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-2) var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	.refresh-time {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.refresh-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-md);
	}

	.refresh-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.refresh-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
