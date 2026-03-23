<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';

	let goal = $state('');
	let submitting = $state(false);
	let error = $state<string | null>(null);

	const canSubmit = $derived(goal.trim().length > 0 && !submitting);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (!canSubmit) return;

		submitting = true;
		error = null;

		try {
			const result = await api.plans.create({ description: goal.trim() });
			await goto(`/plans/${result.slug}`);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to create plan';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="create-form">
	<div class="form-header">
		<Icon name="file-plus" size={24} />
		<h2>New Plan</h2>
	</div>

	<p class="form-hint">
		Describe what you want to build. Semspec will generate a structured plan with
		requirements and scenarios for your review.
	</p>

	<form onsubmit={handleSubmit}>
		<div class="field">
			<label for="plan-goal">What do you want to build?</label>
			<textarea
				id="plan-goal"
				bind:value={goal}
				placeholder="e.g., Add user authentication with JWT tokens, session management, and a login page"
				rows={4}
				disabled={submitting}
				aria-describedby={error ? 'plan-goal-error' : undefined}
			></textarea>
		</div>

		{#if error}
			<div id="plan-goal-error" class="error-banner" role="alert">
				<Icon name="alert-circle" size={14} />
				<span>{error}</span>
			</div>
		{/if}

		<div class="form-actions">
			<a href="/" class="btn-cancel">Cancel</a>
			<button type="submit" class="btn-submit" disabled={!canSubmit} aria-busy={submitting}>
				{#if submitting}
					<Icon name="loader" size={14} />
					Creating...
				{:else}
					<Icon name="arrow-right" size={14} />
					Create Plan
				{/if}
			</button>
		</div>
	</form>

	<div class="tips">
		<h3>Tips for a good plan description</h3>
		<ul>
			<li>Be specific about the feature or change you want</li>
			<li>Mention key technologies or patterns to use</li>
			<li>Include constraints (e.g., "must be backwards compatible")</li>
			<li>Reference existing code if relevant (e.g., "extend the auth module")</li>
		</ul>
	</div>
</div>

<style>
	.create-form {
		max-width: 640px;
		margin: 0 auto;
		padding: var(--space-8) var(--space-6);
	}

	.form-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-2);
		color: var(--color-text-primary);
	}

	.form-header h2 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		margin: 0;
	}

	.form-hint {
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		margin: 0 0 var(--space-6);
		line-height: 1.5;
	}

	.field {
		margin-bottom: var(--space-4);
	}

	.field label {
		display: block;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		margin-bottom: var(--space-2);
	}

	.field textarea {
		width: 100%;
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		font-family: inherit;
		line-height: 1.5;
		resize: vertical;
	}

	.field textarea:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 2px var(--color-accent-muted);
	}

	.field textarea::placeholder {
		color: var(--color-text-muted);
	}

	.field textarea:disabled {
		opacity: 0.6;
	}

	.error-banner {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		margin-bottom: var(--space-4);
	}

	.form-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-3);
	}

	.btn-cancel {
		padding: var(--space-2) var(--space-4);
		background: none;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-decoration: none;
		display: flex;
		align-items: center;
	}

	.btn-cancel:hover {
		background: var(--color-bg-tertiary);
		text-decoration: none;
	}

	.btn-submit {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
	}

	.btn-submit:hover:not(:disabled) {
		opacity: 0.9;
	}

	.btn-submit:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.tips {
		margin-top: var(--space-8);
		padding-top: var(--space-6);
		border-top: 1px solid var(--color-border);
	}

	.tips h3 {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-muted);
		margin: 0 0 var(--space-3);
	}

	.tips ul {
		margin: 0;
		padding-left: var(--space-5);
	}

	.tips li {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		line-height: 1.8;
	}
</style>
