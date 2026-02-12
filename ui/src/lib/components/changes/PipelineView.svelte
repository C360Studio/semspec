<script lang="ts">
	import type { PipelineState, DocumentInfo } from '$lib/types/changes';

	interface Props {
		pipeline: PipelineState;
		documents: DocumentInfo[];
		onStageClick?: (stage: string) => void;
	}

	let { pipeline, documents, onStageClick }: Props = $props();

	const stages = $derived([
		{ key: 'proposal', label: 'Proposal', state: pipeline.proposal },
		{ key: 'design', label: 'Design', state: pipeline.design },
		{ key: 'spec', label: 'Spec', state: pipeline.spec },
		{ key: 'tasks', label: 'Tasks', state: pipeline.tasks }
	]);

	function getDoc(key: string): DocumentInfo | undefined {
		return documents.find((d) => d.type === key);
	}

	function handleClick(key: string) {
		if (onStageClick) {
			onStageClick(key);
		}
	}
</script>

<div class="pipeline-view">
	<div class="pipeline-track">
		{#each stages as stage, i}
			{@const doc = getDoc(stage.key)}
			<button
				class="pipeline-stage"
				data-state={stage.state}
				onclick={() => handleClick(stage.key)}
				disabled={stage.state === 'none'}
			>
				<div class="stage-marker">
					{#if stage.state === 'complete'}
						<span class="marker-icon">\u2713</span>
					{:else if stage.state === 'generating'}
						<span class="marker-icon pulse">\u25CF</span>
					{:else if stage.state === 'failed'}
						<span class="marker-icon">\u2717</span>
					{:else}
						<span class="marker-icon">\u25CB</span>
					{/if}
				</div>
				<span class="stage-label">{stage.label}</span>
				{#if doc?.generated_at}
					<span class="stage-meta">{doc.model}</span>
				{/if}
			</button>
			{#if i < stages.length - 1}
				<div
					class="pipeline-connector"
					class:active={stages[i].state === 'complete'}
				></div>
			{/if}
		{/each}
	</div>
</div>

<style>
	.pipeline-view {
		padding: var(--space-4) 0;
	}

	.pipeline-track {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0;
	}

	.pipeline-stage {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		background: transparent;
		border: none;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.pipeline-stage:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-lg);
	}

	.pipeline-stage:disabled {
		cursor: default;
		opacity: 0.5;
	}

	.pipeline-stage:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.pipeline-stage:focus-visible .stage-marker {
		box-shadow: 0 0 0 4px var(--color-accent-muted);
	}

	.stage-marker {
		width: 40px;
		height: 40px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-full);
		background: var(--color-bg-tertiary);
		border: 2px solid var(--color-border);
		font-size: var(--font-size-lg);
		transition: all var(--transition-fast);
	}

	.pipeline-stage[data-state='complete'] .stage-marker {
		background: var(--color-success-muted);
		border-color: var(--color-success);
		color: var(--color-success);
	}

	.pipeline-stage[data-state='generating'] .stage-marker {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.pipeline-stage[data-state='failed'] .stage-marker {
		background: var(--color-error-muted);
		border-color: var(--color-error);
		color: var(--color-error);
	}

	.pipeline-stage[data-state='none'] .stage-marker {
		color: var(--color-text-muted);
	}

	.marker-icon {
		font-weight: var(--font-weight-semibold);
	}

	.marker-icon.pulse {
		animation: pulse 1.5s ease-in-out infinite;
	}

	.stage-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.stage-meta {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.pipeline-connector {
		width: 60px;
		height: 2px;
		background: var(--color-border);
		margin-bottom: 24px; /* Offset to align with stage marker */
	}

	.pipeline-connector.active {
		background: var(--color-success);
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
