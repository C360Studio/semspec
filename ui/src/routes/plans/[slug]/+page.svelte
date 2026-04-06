<script lang="ts">
	import { page } from '$app/state';
	import { invalidate } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import PlanDetail from '$lib/components/plan/PlanDetail.svelte';
	import RequirementPanel from '$lib/components/plan/RequirementPanel.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import ExecutionTimeline from '$lib/components/trajectory/ExecutionTimeline.svelte';
	import { ReviewDashboard } from '$lib/components/review';
	import { PlanWorkspace } from '$lib/components/workspace';

	// Graph components use browser-only libs (graphology, sigma). Lazy-load to
	// avoid SSR crashes — these are only rendered when viewMode === 'graph'.
	let SigmaCanvas: typeof import('$lib/components/graph/SigmaCanvas.svelte').default | null = $state(null);
	let GraphFilters: typeof import('$lib/components/graph/GraphFilters.svelte').default | null = $state(null);
	import { promotePlan, executePlan, retryFailed } from '$lib/actions/plans';
	import { derivePlanPipeline, getStageLabel } from '$lib/types/plan';
	import { feedStore, syncQuestionsToFeed } from '$lib/stores/feed.svelte';
	import { graphStore } from '$lib/stores/graphStore.svelte';
	import type { GraphStoreAdapter } from '$lib/stores/graphStore.svelte';
	import { graphApi } from '$lib/services/graphApi';
	import { transformPathSearchResult, transformGlobalSearchResult } from '$lib/services/graphTransform';
	import type { ClassificationMeta } from '$lib/api/graph-types';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const slug = $derived(page.params.slug);
	const plan = $derived(data.plan);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);
	const requirements = $derived(data.requirements);
	const scenariosByReq = $derived(data.scenariosByReq);
	const hasRequirements = $derived(requirements.length > 0);
	const hasScenarios = $derived(Object.values(scenariosByReq).some((s) => s.length > 0));

	// ---------------------------------------------------------------------------
	// View mode — toggle between Doc, Graph, and Files
	// ---------------------------------------------------------------------------
	type ViewMode = 'doc' | 'graph' | 'files';
	let viewMode = $state<ViewMode>('doc');

	// Build a plan-scoped graph adapter that loads the plan's entity neighborhood
	const planGraphAdapter: GraphStoreAdapter = {
		async listEntities({ limit = 50 }) {
			// Load entities connected to this plan via pathSearch
			const planEntityId = `semspec.plan.${slug}`;
			const result = await graphApi.pathSearch(planEntityId, 2, limit);
			const entities = transformPathSearchResult(result);
			return { entities };
		},
		async getEntityNeighbors(entityId: string) {
			const result = await graphApi.pathSearch(entityId, 2, 50);
			const entities = transformPathSearchResult(result);
			return { entities };
		},
		async searchEntities({ query, limit = 100 }) {
			const result = await graphApi.globalSearch(query);
			const allEntities = transformGlobalSearchResult(result);
			lastClassification = result.classification ?? null;
			return { entities: allEntities.slice(0, limit) };
		}
	};

	let lastClassification = $state<ClassificationMeta | null>(null);
	let nlqSearching = $state(false);

	async function setViewMode(mode: ViewMode) {
		if (mode === 'graph') {
			// Lazy-load graph components on first use (browser-only libs)
			if (!SigmaCanvas) {
				const [sc, gf] = await Promise.all([
					import('$lib/components/graph/SigmaCanvas.svelte'),
					import('$lib/components/graph/GraphFilters.svelte')
				]);
				SigmaCanvas = sc.default;
				GraphFilters = gf.default;
			}
			graphStore.setGraphMode(true, slug);
			if (graphStore.entities.size === 0) {
				graphStore.loadInitialGraph(planGraphAdapter);
			}
		} else {
			graphStore.setGraphMode(false);
		}
		viewMode = mode;
	}

	// Turn off graph mode and clear plan-scoped entities when navigating away.
	// Without clearEntities(), the /entities page would see stale plan-scoped
	// data and skip its initial load (it guards on entities.size > 0).
	$effect(() => {
		return () => {
			graphStore.setGraphMode(false);
			graphStore.clearEntities();
		};
	});

	// Graph event handlers
	const filteredEntities = $derived(graphStore.filteredEntities);
	const filteredRelationships = $derived(graphStore.filteredRelationships);

	function handleEntitySelect(entityId: string | null) {
		graphStore.selectEntity(entityId);
	}

	function handleEntityHover(entityId: string | null) {
		graphStore.setHoveredEntity(entityId);
	}

	async function handleEntityExpand(entityId: string) {
		await graphStore.expandEntity(planGraphAdapter, entityId);
	}

	async function handleGraphRefresh() {
		lastClassification = null;
		graphStore.clearEntities();
		await graphStore.loadInitialGraph(planGraphAdapter);
	}

	function handleToggleType(type: string) {
		graphStore.toggleEntityType(type);
	}

	function handleSearchChange(search: string) {
		graphStore.setFilters({ search });
	}

	async function handleNlqSearch(query: string) {
		nlqSearching = true;
		lastClassification = null;
		try {
			await graphStore.searchEntities(planGraphAdapter, query);
		} finally {
			nlqSearching = false;
		}
	}

	// Reset files mode if plan becomes unapproved (e.g. via invalidate)
	$effect(() => {
		if (plan && !plan.approved && viewMode === 'files') {
			viewMode = 'doc';
		}
	});

	// Approved plans show requirements and reviews
	const showApprovedContent = $derived(plan?.approved === true);

	// Stages where reviews are most relevant — expand by default
	const REVIEW_FOCUS_STAGES = new Set(['scenarios_generated', 'ready_for_execution', 'ready_for_approval']);
	// Stages where execution is active — collapse reviews by default
	const EXECUTING_STAGES = new Set(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed']);

	const reviewsDefaultExpanded = $derived(
		plan ? REVIEW_FOCUS_STAGES.has(plan.stage) : false
	);

	// User can override the default — sticky toggle
	let reviewsUserToggle = $state<boolean | null>(null);

	const reviewsExpanded = $derived(
		reviewsUserToggle !== null ? reviewsUserToggle : reviewsDefaultExpanded
	);

	// Reset user toggle when plan stage changes to a decisive stage
	$effect(() => {
		if (plan && EXECUTING_STAGES.has(plan.stage)) {
			reviewsUserToggle = null;
		}
	});

	function toggleReviews() {
		reviewsUserToggle = !reviewsExpanded;
	}

	let actionError = $state<string | null>(null);

	async function handlePromote() {
		if (!plan) return;
		actionError = null;
		try {
			await promotePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to approve plan';
		}
	}

	async function handleExecute() {
		if (!plan) return;
		actionError = null;
		try {
			await executePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to start execution';
		}
	}

	async function handleReplay() {
		if (!plan) return;
		actionError = null;
		try {
			await executePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to replay';
		}
	}

	async function handleRetryFailed() {
		if (!plan) return;
		actionError = null;
		try {
			await retryFailed(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to retry';
		}
	}

	async function handleRefresh() {
		await invalidate('app:plans');
	}

	// Stages that represent structural plan transitions — load function data changes
	// at these boundaries (new requirements, scenarios, execution results, etc.).
	// Within-stage SSE events (task/requirement updates) only affect the activity
	// feed display and do not require a REST re-fetch.
	const STRUCTURAL_STAGES = new Set([
		'drafted',
		'reviewed',
		'approved',
		'requirements_generated',
		'scenarios_generated',
		'scenarios_reviewed',
		'ready_for_execution',
		'complete',
		'failed'
	]);

	// Feed store: connects plan + execution SSE for the Activity Feed panel.
	// Invalidates load function data only on structural plan stage transitions,
	// not on every SSE event (task/requirement updates feed the display, not REST).
	$effect(() => {
		const currentSlug = slug;
		if (!currentSlug || typeof window === 'undefined') return;

		feedStore.connectPlan(currentSlug);

		const unsubPlan = feedStore.onPlanStageChange((newStage) => {
			if (STRUCTURAL_STAGES.has(newStage)) {
				invalidate('app:plans');
			}
		});

		return () => {
			unsubPlan();
			feedStore.disconnectPlan();
		};
	});

	// Sync questions into the feed store.
	$effect(() => {
		syncQuestionsToFeed();
	});
</script>

<svelte:head>
	<title>{plan?.title || plan?.slug || 'Plan'} - Semspec</title>
</svelte:head>

<div class="plan-detail">
	<header class="detail-header">
		<a href="/" class="back-link">
			<Icon name="chevron-left" size={16} />
			Back
		</a>
		{#if plan}
			<div class="header-info">
				<h1 class="plan-title">{plan.title || plan.slug}</h1>
				<div class="plan-meta">
					<ModeIndicator approved={plan.approved} />
					<span class="plan-stage" data-stage={plan.stage}>
						{getStageLabel(plan.stage)}
					</span>
				</div>
			</div>
			<div class="view-toggle" role="group" aria-label="View mode">
				<button
					class="toggle-btn"
					class:active={viewMode === 'doc'}
					aria-pressed={viewMode === 'doc'}
					onclick={() => setViewMode('doc')}
				>
					<Icon name="file-text" size={14} />
					<span>Doc</span>
				</button>
				<button
					class="toggle-btn"
					class:active={viewMode === 'graph'}
					aria-pressed={viewMode === 'graph'}
					onclick={() => setViewMode('graph')}
				>
					<Icon name="git-merge" size={14} />
					<span>Graph</span>
				</button>
				{#if plan.approved}
					<button
						class="toggle-btn"
						class:active={viewMode === 'files'}
						aria-pressed={viewMode === 'files'}
						onclick={() => setViewMode('files')}
					>
						<Icon name="folder" size={14} />
						<span>Files</span>
					</button>
				{/if}
			</div>
		{/if}
	</header>

	{#if !plan}
		<div class="not-found">
			<Icon name="alert-circle" size={48} />
			<h2>Plan not found</h2>
			<p>The plan "{slug}" could not be found.</p>
			<a href="/" class="btn btn-primary">Back to Board</a>
		</div>
	{:else if viewMode === 'graph' && SigmaCanvas && GraphFilters}
		<div class="graph-content">
			<GraphFilters
				visibleTypes={graphStore.visibleTypes}
				presentTypes={graphStore.presentEntityTypes}
				search={graphStore.filters.search}
				visibleCount={filteredEntities.length}
				totalCount={graphStore.entities.size}
				classification={lastClassification}
				searching={nlqSearching}
				onToggleType={handleToggleType}
				onSearchChange={handleSearchChange}
				onNlqSearch={handleNlqSearch}
				onShowAll={() => graphStore.showAllTypes()}
				onHideAll={() => graphStore.hideAllTypes()}
			/>

			{#if graphStore.error}
				<div class="error-banner" role="alert">
					<Icon name="alert-circle" size={14} />
					<span>{graphStore.error}</span>
					<button class="error-dismiss" onclick={() => graphStore.setError(null)} aria-label="Dismiss">×</button>
				</div>
			{/if}

			<div class="canvas-wrapper">
				<SigmaCanvas
					entities={filteredEntities}
					relationships={filteredRelationships}
					selectedEntityId={graphStore.selectedEntityId}
					hoveredEntityId={graphStore.hoveredEntityId}
					onEntitySelect={handleEntitySelect}
					onEntityHover={handleEntityHover}
					onEntityExpand={handleEntityExpand}
					onRefresh={handleGraphRefresh}
					loading={graphStore.loading}
				/>
			</div>

			<div class="graph-footer">
				<a href="/entities" class="explorer-link">
					<Icon name="maximize-2" size={12} />
					<span>Open full explorer</span>
				</a>
			</div>
		</div>
	{:else if viewMode === 'files'}
		<div class="files-content">
			<PlanWorkspace slug={plan.slug} />
		</div>
	{:else}
		<div class="plan-content">
			<!-- Action bar: approve / execute / status -->
			{#if plan.goal || plan.approved}
				<div class="action-row">
					{#if plan.approved && pipeline}
						<PipelineIndicator
							plan={pipeline.plan}
							requirements={pipeline.requirements}
							execute={pipeline.execute}
						/>
					{/if}
					<ActionBar
						{plan}
						{hasRequirements}
						{hasScenarios}
						onPromote={handlePromote}
						onExecute={handleExecute}
						onReplay={handleReplay}
						onRetryFailed={handleRetryFailed}
					/>
				</div>
			{/if}

			{#if actionError}
				<div class="error-banner" role="alert">
					<Icon name="alert-circle" size={14} />
					<span>{actionError}</span>
				</div>
			{/if}

			<!-- Agent pipeline during execution -->
			{#if plan.active_loops && plan.active_loops.length > 0}
				<div class="pipeline-section">
					<AgentPipelineView slug={plan.slug} loops={plan.active_loops} />
				</div>
			{/if}

			<!-- Plan details: goal, context, scope -->
			<PlanDetail {plan} phases={[]} requirements={requirements} onRefresh={handleRefresh} />

			<!-- Review Dashboard: inline collapsible, shown after plan is approved -->
			{#if showApprovedContent}
				<div class="review-section">
					<button class="section-toggle" onclick={toggleReviews} aria-expanded={reviewsExpanded} aria-label={reviewsExpanded ? 'Collapse reviews section' : 'Expand reviews section'}>
						<div class="section-toggle-left">
							<Icon name={reviewsExpanded ? 'chevron-down' : 'chevron-right'} size={14} />
							<Icon name="list-checks" size={14} />
							<span class="section-toggle-title">Reviews</span>
						</div>
					</button>
					{#if reviewsExpanded}
						<div class="review-body">
							<ReviewDashboard slug={plan.slug} result={data.reviews ?? undefined} autoFetch={false} />
						</div>
					{/if}
				</div>
			{/if}

			<!-- Trajectory timeline: plan phase + execution loops -->
			<ExecutionTimeline slug={plan.slug} stage={plan.stage} trajectoryItems={data.trajectoryItems} />

			<!-- Requirements + Scenarios are shown inline within PlanDetail above -->
		</div>
	{/if}
</div>

<style>
	.plan-detail {
		padding: var(--space-4) var(--space-6);
		max-width: 900px;
		margin: 0 auto;
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.detail-header {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		margin-bottom: var(--space-4);
		flex-shrink: 0;
	}

	.back-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-muted);
		text-decoration: none;
		font-size: var(--font-size-sm);
		flex-shrink: 0;
	}

	.back-link:hover {
		color: var(--color-text-primary);
	}

	.header-info {
		flex: 1;
		min-width: 0;
	}

	.plan-title {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.plan-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}

	.plan-stage {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.plan-stage[data-stage='implementing'],
	.plan-stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='ready_for_execution'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.not-found {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-12) 0;
		color: var(--color-text-muted);
		text-align: center;
	}

	.not-found h2 {
		margin: 0;
		color: var(--color-text-primary);
	}

	.plan-content {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.action-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-lg);
		flex-shrink: 0;
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
	}

	.pipeline-section {
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
	}

	/* Collapsible review section — mirrors ExecutionTimeline phase-section pattern */
	.review-section {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.section-toggle {
		display: flex;
		align-items: center;
		justify-content: space-between;
		width: 100%;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: none;
		cursor: pointer;
		transition: background var(--transition-fast);
	}

	.section-toggle:hover {
		background: var(--color-bg-elevated);
	}

	.section-toggle-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-secondary);
	}

	.section-toggle-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.review-body {
		padding: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		text-decoration: none;
		cursor: pointer;
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover {
		opacity: 0.9;
	}

	/* View toggle (Doc / Graph) */
	.view-toggle {
		display: flex;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		padding: 2px;
		flex-shrink: 0;
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		border: none;
		background: none;
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn:hover {
		color: var(--color-text-primary);
	}

	.toggle-btn.active {
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
	}

	/* Graph mode content */
	.graph-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.canvas-wrapper {
		flex: 1;
		min-height: 0;
		position: relative;
	}

	.graph-footer {
		display: flex;
		justify-content: flex-end;
		padding: var(--space-1) var(--space-2);
		border-top: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.explorer-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		text-decoration: none;
	}

	.explorer-link:hover {
		color: var(--color-text-primary);
	}

	.error-dismiss {
		margin-left: auto;
		background: transparent;
		border: none;
		color: inherit;
		font-size: 16px;
		cursor: pointer;
		padding: 0 4px;
		opacity: 0.7;
		line-height: 1;
	}

	.error-dismiss:hover {
		opacity: 1;
	}

	/* Files mode content */
	.files-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	@media (max-width: 768px) {
		.plan-detail {
			padding: var(--space-3);
		}

		.action-row {
			flex-direction: column;
			align-items: stretch;
		}
	}
</style>
