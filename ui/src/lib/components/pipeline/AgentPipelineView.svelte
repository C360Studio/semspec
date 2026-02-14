<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import PipelineStage from './PipelineStage.svelte';
	import type { PipelineState, PipelineStageState } from '$lib/types/pipeline';
	import { getMainStages, getParallelStages, createInitialPipelineState, derivePipelineFromLoops } from '$lib/types/pipeline';

	/** Minimal loop interface for pipeline visualization */
	interface LoopLike {
		loop_id: string;
		role?: string;
		state: string;
		iterations?: number;
		max_iterations?: number;
	}

	interface Props {
		/** Plan/workflow slug */
		slug: string;
		/** Optional pre-computed pipeline state */
		pipelineState?: PipelineState;
		/** Active loops to derive state from */
		loops?: LoopLike[];
		/** Compact mode */
		compact?: boolean;
		/** Show review branch (parallel reviewers) */
		showReviewBranch?: boolean;
	}

	let { slug, pipelineState, loops, compact = false, showReviewBranch = true }: Props = $props();

	// Derive pipeline state from loops if not provided
	const state = $derived.by(() => {
		if (pipelineState) return pipelineState;
		if (loops) {
			return derivePipelineFromLoops(
				slug,
				loops.map((l) => ({
					role: l.role,
					state: l.state,
					iterations: l.iterations,
					max_iterations: l.max_iterations,
					loop_id: l.loop_id
				}))
			);
		}
		return createInitialPipelineState(slug);
	});

	// Get main pipeline stages (non-parallel)
	const mainStages = $derived(getMainStages());

	// Get stage state by ID
	function getStageState(stageId: string): PipelineStageState | undefined {
		return state.stages.find((s) => s.stage.id === stageId);
	}

	// Get parallel reviewer stages
	const parallelReviewers = $derived(getParallelStages('spec_reviewer'));

	// Check if we should show the review branch
	const specReviewerState = $derived(getStageState('spec_reviewer'));
	const showBranch = $derived(
		showReviewBranch &&
		specReviewerState &&
		(specReviewerState.state === 'complete' || specReviewerState.state === 'active' ||
		 parallelReviewers.some((r) => {
			const s = getStageState(r.id);
			return s && s.state !== 'pending';
		 }))
	);
</script>

<div class="pipeline-view" class:compact>
	<div class="main-pipeline">
		{#each mainStages as stageDef, index}
			{@const stageState = getStageState(stageDef.id)}
			{#if stageState}
				{#if index > 0}
					<div class="connector" class:active={stageState.state === 'active' || stageState.state === 'complete'}>
						<Icon name="chevron-right" size={compact ? 12 : 14} />
					</div>
				{/if}
				<PipelineStage {stageState} {compact} />
			{/if}
		{/each}
	</div>

	{#if showBranch && parallelReviewers.length > 0}
		<div class="review-branch">
			<div class="branch-connector">
				<div class="branch-line"></div>
				<div class="branch-split">
					{#each parallelReviewers as reviewerDef, index}
						{@const reviewerState = getStageState(reviewerDef.id)}
						{#if reviewerState}
							<div class="branch-arm">
								{#if index === 0}
									<div class="arm-line top"></div>
								{:else if index === parallelReviewers.length - 1}
									<div class="arm-line bottom"></div>
								{:else}
									<div class="arm-line middle"></div>
								{/if}
								<PipelineStage stageState={reviewerState} {compact} />
							</div>
						{/if}
					{/each}
				</div>
			</div>
		</div>
	{/if}
</div>

<style>
	.pipeline-view {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.main-pipeline {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex-wrap: wrap;
	}

	.connector {
		display: flex;
		align-items: center;
		color: var(--color-text-muted);
		padding: 0 var(--space-1);
	}

	.connector.active {
		color: var(--color-success);
	}

	.compact .connector {
		padding: 0;
	}

	.review-branch {
		margin-left: var(--space-6);
		padding-left: var(--space-4);
	}

	.compact .review-branch {
		margin-left: var(--space-4);
		padding-left: var(--space-2);
	}

	.branch-connector {
		display: flex;
		align-items: flex-start;
	}

	.branch-line {
		width: 20px;
		height: 2px;
		background: var(--color-border);
		margin-top: 24px;
	}

	.compact .branch-line {
		width: 12px;
		margin-top: 18px;
	}

	.branch-split {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.branch-arm {
		display: flex;
		align-items: center;
	}

	.arm-line {
		width: 16px;
		height: 2px;
		background: var(--color-border);
		position: relative;
	}

	.arm-line.top::before,
	.arm-line.bottom::before,
	.arm-line.middle::before {
		content: '';
		position: absolute;
		left: 0;
		width: 2px;
		background: var(--color-border);
	}

	.arm-line.top::before {
		top: 0;
		height: 24px;
	}

	.arm-line.bottom::before {
		bottom: 0;
		height: 24px;
	}

	.arm-line.middle::before {
		top: -24px;
		height: 50px;
	}

	.compact .arm-line {
		width: 10px;
	}

	.compact .arm-line.top::before {
		height: 18px;
	}

	.compact .arm-line.bottom::before {
		height: 18px;
	}

	.compact .arm-line.middle::before {
		top: -18px;
		height: 38px;
	}
</style>
