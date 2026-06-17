<script lang="ts">
	import { page } from '$app/state';
	import { invalidate } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import PlanDetail from '$lib/components/plan/PlanDetail.svelte';
	import PlanReviewCard from '$lib/components/plan/PlanReviewCard.svelte';
	import InProgressPanel from '$lib/components/plan/InProgressPanel.svelte';
	import { deriveGuidance } from '$lib/components/plan/guidance';
	import RequirementPanel from '$lib/components/plan/RequirementPanel.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import PhaseArtifactsView from '$lib/components/plan/PhaseArtifactsView.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import ExecutionTimeline from '$lib/components/trajectory/ExecutionTimeline.svelte';
	import { ReviewDashboard } from '$lib/components/review';
	import { PlanWorkspace } from '$lib/components/workspace';

	// Graph components use browser-only libs (graphology, sigma). Lazy-load to
	// avoid SSR crashes — these are only rendered when viewMode === 'graph'.
	let SigmaCanvas: typeof import('$lib/components/graph/SigmaCanvas.svelte').default | null = $state(null);
	let GraphFilters: typeof import('$lib/components/graph/GraphFilters.svelte').default | null = $state(null);
	import { promotePlan, executePlan, retryFailed } from '$lib/actions/plans';
	import RetrySelectedPicker from '$lib/components/plan/RetrySelectedPicker.svelte';
	import { derivePlanPipeline, getStageLabel } from '$lib/types/plan';
	import { activePhaseProgress } from '$lib/types/activePlanProgress';
	import { selectFreshestPlan } from '$lib/types/planFreshness';
	import { mergeLiveTrajectoryItems } from '$lib/types/trajectoryActivityProjection';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { feedStore, syncQuestionsToFeed } from '$lib/stores/feed.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { graphStore } from '$lib/stores/graphStore.svelte';
	import type { GraphStoreAdapter } from '$lib/stores/graphStore.svelte';
	import type { PlanPhaseSummary } from '$lib/types/feed';
	import { graphApi } from '$lib/services/graphApi';
	import { transformPathSearchResult, transformGlobalSearchResult } from '$lib/services/graphTransform';
	import type { ClassificationMeta } from '$lib/api/graph-types';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const slug = $derived(page.params.slug);
	// Prefer the SSE-fed feedStore mirror for the active slug — it carries the
	// same PlanWithStatus shape the loader returns, and updates without a
	// full loader re-run. Fall back to load-function data for initial render
	// (before the first SSE event arrives) and for stale slugs. When loader
	// data has advanced farther than the SSE mirror, prefer the loader; this
	// prevents a stale "Generating scenarios" header from coexisting with
	// freshly loaded execution trajectories.
	const plan = $derived(
		selectFreshestPlan(feedStore.currentPlan, data.plan, slug ?? '')
	);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);
	const requirements = $derived(data.requirements);
	const scenariosByReq = $derived(data.scenariosByReq);
	const hasRequirements = $derived(requirements.length > 0);
	const hasScenarios = $derived(Object.values(scenariosByReq).some((s) => s.length > 0));
	const liveTrajectoryItems = $derived(
		mergeLiveTrajectoryItems(data.trajectoryItems, activityStore.recent, slug ?? '')
	);

	// ---------------------------------------------------------------------------
	// View mode — toggle between Doc, Graph, and Files
	// ---------------------------------------------------------------------------
	type ViewMode = 'doc' | 'artifacts' | 'graph' | 'files';
	const FILES_VIEW_STAGES = new Set([
		'implementing',
		'executing',
		'ready_for_qa',
		'reviewing_qa',
		'reviewing_rollup',
		'complete',
		'complete_with_deferrals',
		'failed'
	]);
	let viewMode = $state<ViewMode>('doc');
	let viewModeRequest = 0;
	const showFilesView = $derived(
		plan
			? FILES_VIEW_STAGES.has(plan.stage) ||
				['execution', 'qa', 'terminal'].includes(plan.phase_summary?.phase ?? '')
			: false
	);

	// Build a plan-scoped graph adapter that loads the plan's entity neighborhood
	const planGraphAdapter: GraphStoreAdapter = {
		async listEntities({ limit = 50 }) {
			// Load entities connected to this plan via pathSearch. Depth=3 is
			// required to cover plan → requirement → scenario → dag-node — depth=2
			// stopped at requirements and rendered a near-empty view (bug #7.3).
			const planEntityId = plan?.id ?? `semspec.plan.${slug}`;
			const result = await graphApi.pathSearch(planEntityId, 3, limit);
			const entities = transformPathSearchResult(result);
			return { entities };
		},
		async getEntityNeighbors(entityId: string) {
			// Depth=3 matches the initial load; keeps expand-by-click consistent
			// with what the plan graph shows on first paint.
			const result = await graphApi.pathSearch(entityId, 3, 50);
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
		const request = ++viewModeRequest;
		viewMode = mode;
		if (mode === 'graph') {
			graphStore.setGraphMode(true, slug);
			// Lazy-load graph components on first use (browser-only libs)
			if (!SigmaCanvas) {
				const [sc, gf] = await Promise.all([
					import('$lib/components/graph/SigmaCanvas.svelte'),
					import('$lib/components/graph/GraphFilters.svelte')
				]);
				if (request !== viewModeRequest || viewMode !== 'graph') return;
				SigmaCanvas = sc.default;
				GraphFilters = gf.default;
			}
			if (request === viewModeRequest && viewMode === 'graph' && graphStore.entities.size === 0) {
				await graphStore.loadInitialGraph(planGraphAdapter);
			}
		} else {
			graphStore.setGraphMode(false);
		}
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

	// Reset files mode if the plan is not in an execution/result stage.
	$effect(() => {
		if (plan && !showFilesView && viewMode === 'files') {
			viewMode = 'doc';
		}
	});

	// Approved plans show requirements and reviews
	const showApprovedContent = $derived(plan?.approved === true);

	// Guidance for the active LLM phase. Reused for the prominent in-progress
	// panel above PlanDetail. PlanDetail still renders its own small chip via
	// the same predicate so the existing small-form factor remains for cases
	// where the big panel is hidden.
	const planGuidance = $derived(
		plan ? deriveGuidance(plan.approved, plan.stage, requirements.length) : null
	);
	const activeProgress = $derived(activePhaseProgress(liveTrajectoryItems));
	const progressPanel = $derived.by(() => {
		if (plan?.phase_summary && shouldShowPhaseSummaryBanner(plan.phase_summary)) {
			return {
				title: plan.phase_summary.title,
				detail: phaseSummaryDetail(plan.phase_summary),
				phase: plan.phase_summary.phase,
				state: plan.phase_summary.state,
				startedAt: activeProgress?.startedAt ?? stageStartedAt(plan)
			};
		}
		if (activeProgress) {
			return {
				title: activeProgress.title,
				detail: activeProgress.detail,
				phase: 'activity',
				state: 'active',
				startedAt: activeProgress.startedAt
			};
		}
		if (planGuidance?.isLoading && plan) {
			return {
				title: stageTitle(plan.stage),
				detail: planGuidance.message,
				phase: 'planning',
				state: 'active',
				startedAt: stageStartedAt(plan)
			};
		}
		return null;
	});

	function shouldShowPhaseSummaryBanner(summary: PlanPhaseSummary): boolean {
		if (summary.phase === 'terminal') return false;
		if (summary.state === 'active' || summary.state === 'waiting') return true;
		return summary.phase === 'execution' || summary.phase === 'recovery' || summary.phase === 'qa';
	}

	function phaseSummaryDetail(summary: PlanPhaseSummary): string | undefined {
		if (summary.detail) return summary.detail;
		if (summary.wait?.policy_reason) return summary.wait.policy_reason;
		if (summary.recovery?.summary) return summary.recovery.summary;
		if (summary.qa?.summary) return summary.qa.summary;
		return undefined;
	}

	// Pick the best available timestamp to drive the in-progress panel's
	// elapsed-time ticker for the CURRENT stage. The plan API doesn't yet
	// expose a `stage_started_at` field, so we fall back through the
	// available timestamps in order of relevance per phase. Without this,
	// passing `plan.created_at` to a `reviewing_qa` plan that was created
	// hours ago would render "4h 12m" for what is actually a 3-minute QA
	// run — the inflated time reads exactly as wedged, which is the UX
	// problem the in-progress panel was added to solve. Caught
	// 2026-05-21 by svelte-reviewer.
	function stageStartedAt(p: typeof plan): string | null {
		if (!p) return null;
		switch (p.stage) {
			case 'reviewing_qa':
			case 'reviewing_rollup':
			case 'reviewing_scenarios':
			case 'generating_requirements':
			case 'generating_architecture':
			case 'preparing_stories':
			case 'stories_generated':
			case 'generating_scenarios':
			case 'ready_for_execution':
			case 'implementing':
			case 'ready_for_qa':
				// Post-approval phases: approved_at is the closest stage-start
				// proxy we have. Falls back to reviewed_at, then created_at.
				return p.approved_at ?? p.reviewed_at ?? p.created_at;
			case 'drafted':
			case 'reviewing_draft':
				// Drafted is set when the planner finishes; reviewed_at fires
				// when plan-reviewer claims (or completes). Either is closer
				// than created_at for a multi-iteration draft.
				return p.reviewed_at ?? p.created_at;
			default:
				return p.created_at;
		}
	}

	// Headline label for the in-progress panel keyed by plan.stage. Falls back
	// to a generic "Working…" so the panel still renders if a future stage
	// adds an isLoading guidance entry but hasn't been wired here yet.
	function stageTitle(stage: string | undefined): string {
		switch (stage) {
			case 'drafting':
				return 'Drafting plan…';
			case 'drafted':
				return 'Plan drafted — awaiting reviewer';
			case 'reviewing_draft':
				return 'Reviewing plan draft…';
			case 'approved':
				// Falls through — `approved` is the brief window between plan
				// approval and the requirement-generator claiming the work, so
				// it gets the same "Generating requirements…" copy.
			case 'generating_requirements':
				return 'Generating requirements…';
			case 'generating_architecture':
				return 'Generating architecture…';
			case 'preparing_stories':
				return 'Preparing Stories…';
			case 'stories_generated':
				return 'Stories generated';
			case 'requirements_generated':
			case 'generating_scenarios':
				return 'Generating scenarios…';
			case 'ready_for_execution':
				return 'Ready for execution';
			case 'implementing':
			case 'executing':
				return 'Execution…';
			case 'reviewing_scenarios':
				return 'Reviewing scenarios…';
			case 'reviewing_qa':
				return 'Running tests…';
			case 'reviewing_rollup':
				return 'Reviewing rollup…';
			default:
				return 'Working…';
		}
	}

	// Stages where reviews are most relevant — expand by default
	const REVIEW_FOCUS_STAGES = new Set(['scenarios_generated', 'ready_for_execution', 'ready_for_approval']);

	const reviewsDefaultExpanded = $derived(
		plan ? REVIEW_FOCUS_STAGES.has(plan.stage) : false
	);

	// User can override the default — sticky toggle
	let reviewsUserToggle = $state<boolean | null>(null);

	const reviewsExpanded = $derived(
		reviewsUserToggle !== null ? reviewsUserToggle : reviewsDefaultExpanded
	);

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
	//
	// REDUCED 2026-05-19: previously included drafted/reviewed/approved/
	// scenarios_reviewed/ready_for_execution/failed — but the plan content for
	// those is now mirrored into feedStore.currentPlan directly from the SSE
	// payload, so no loader re-run is needed. We only invalidate when a
	// dependent COLLECTION (requirements, scenarios, trajectories, reviews)
	// changes shape — these aren't in the plan SSE payload. The previous
	// 6-stage invalidate storm caused 1-3s browser lockups per transition;
	// the trimmed list reduces that to ~3 targeted refetches per plan
	// lifecycle (caught during the 2026-05-19 demo session).
	const STRUCTURAL_STAGES = new Set([
		'requirements_generated', // requirements list appears
		'scenarios_generated', // scenarios per requirement appear
		'implementing', // trajectories start populating
		'complete' // reviews finalize
	]);

	// Feed store: connects plan + execution SSE for the Activity Feed panel.
	// Invalidates load function data only on structural plan stage transitions
	// where dependent COLLECTIONS change. Other stage transitions update plan
	// content via feedStore.currentPlan (see `plan` derivation above) without
	// any REST round-trip.
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

	// Re-sync when the questions list changes. questionsStore.all is the explicit
	// dependency; syncQuestionsToFeed untrack()s its own derived reads so only
	// this single dependency drives the effect.
	$effect(() => {
		void questionsStore.all;
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
					<span class="plan-stage" data-stage={plan.stage} data-phase={plan.phase_summary?.phase ?? plan.stage}>
						{plan.phase_summary?.title ?? getStageLabel(plan.stage)}
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
					class:active={viewMode === 'artifacts'}
					aria-pressed={viewMode === 'artifacts'}
					onclick={() => setViewMode('artifacts')}
				>
					<Icon name="book-open" size={14} />
					<span>Artifacts</span>
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
				{#if showFilesView}
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
	{:else if viewMode === 'graph'}
		<div class="view-loading" role="status">
			<Icon name="loader" size={20} />
			<span>Loading graph...</span>
		</div>
	{:else if viewMode === 'files'}
		<div class="files-content">
			<PlanWorkspace slug={plan.slug} />
		</div>
	{:else if viewMode === 'artifacts'}
		<div class="artifacts-content">
			<PhaseArtifactsView slug={plan.slug} />
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
				<!-- Per-requirement retry picker — surfaces when the plan has at
				     least one failed requirement so the user can cherry-pick
				     which failures to retry, instead of the coarse "retry all
				     failed" button. Sits under the ActionBar on stalled plans.
				     Invariant: this mounts only for stage='implementing';
				     ActionBar's "Retry Failed" button renders for stage='failed'.
				     The two stages are disjoint so picker + ActionBar-retry
				     never appear simultaneously today. If a future stage-map
				     change overlaps them, either gate the ActionBar button or
				     replace it with the picker. -->
				{#if plan.execution_summary && plan.execution_summary.failed > 0 && plan.stage === 'implementing'}
					<RetrySelectedPicker
						slug={plan.slug}
						onRetried={handleRefresh}
					/>
				{/if}
			{/if}

			{#if actionError}
				<div class="error-banner" role="alert">
					<Icon name="alert-circle" size={14} />
					<span>{actionError}</span>
				</div>
			{/if}

			<!-- In-progress callout for active LLM phases. Without this, the
			     plan-detail body during drafting/reviewing/generating reads as
			     empty ("nothing's happening") because PlanDetail's goal/
			     context/scope sections haven't been populated yet. Reuses
			     planGuidance — same predicate that gates the small chip
			     inside PlanDetail — so the panel and chip stay in sync. -->
			{#if progressPanel}
				<InProgressPanel
					title={progressPanel.title}
					detail={progressPanel.detail}
					phase={progressPanel.phase}
					phaseState={progressPanel.state}
					startedAt={progressPanel.startedAt}
				/>
			{/if}

			<!-- Agent pipeline during execution -->
			{#if plan.active_loops && plan.active_loops.length > 0}
				<div class="pipeline-section">
					<AgentPipelineView slug={plan.slug} loops={plan.active_loops} />
				</div>
			{/if}

			<!-- Plan details: goal, context, scope -->
			<PlanDetail {plan} phases={[]} requirements={requirements} onRefresh={handleRefresh} />

			<!-- Reviews: collapsible. R1 plan-reviewer verdict appears as soon as the
			     planner+reviewer have finished (plan.review_verdict is set on the plan
			     itself). Implementation-time code-review aggregation (ReviewDashboard)
			     populates later, after requirements + scenarios + tasks have executed.
			     Both render under the same Reviews header so users find them where the
			     mental model expects "reviews" to be. -->
			{#if plan.review_verdict || showApprovedContent}
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
							{#if plan.review_verdict}
								<PlanReviewCard
									verdict={plan.review_verdict}
									summary={plan.review_summary}
									reviewedAt={plan.reviewed_at}
									iteration={plan.review_iteration}
								/>
							{/if}
							{#if showApprovedContent}
								<ReviewDashboard slug={plan.slug} result={data.reviews ?? undefined} autoFetch={false} />
							{/if}
						</div>
					{/if}
				</div>
			{/if}

			<!-- Trajectory timeline: plan phase + execution loops -->
			<ExecutionTimeline slug={plan.slug} stage={plan.stage} trajectoryItems={liveTrajectoryItems} />

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
		/* min-height: 0 lets .plan-content (flex:1; overflow-y:auto) actually
		 * scroll. Without it the flex item's default min-height:auto expands
		 * to fit content, defeating overflow:auto — so dynamically added SSE
		 * content silently extends below the viewport with no scrollbar
		 * update. Caught 2026-05-19 during gemini-stack walk-through. */
		min-height: 0;
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
	.plan-stage[data-stage='executing'],
	.plan-stage[data-phase='execution'] {
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

	.view-loading {
		flex: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		min-height: 240px;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.view-loading :global(svg) {
		animation: spin 1.6s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
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

	/* Artifacts mode content — scrolls vertically so the sticky TOC can pin. */
	.artifacts-content {
		flex: 1;
		overflow-y: auto;
		padding: 0 var(--space-2);
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
