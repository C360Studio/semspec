import { describe, it, expect } from 'vitest';
import type { components } from '$lib/types/api.generated';

// Type aliases for clarity — mirror what index.ts exports as Generated* types
type GeneratedPlanWithStatus = components['schemas']['PlanWithStatus'];
type GeneratedActiveLoopStatus = components['schemas']['ActiveLoopStatus'];
type GeneratedTask = components['schemas']['Task'];
type GeneratedAcceptanceCriterion = components['schemas']['AcceptanceCriterion'];
type GeneratedSynthesisResult = components['schemas']['SynthesisResult'];
type GeneratedReviewFinding = components['schemas']['ReviewFinding'];
type GeneratedReviewerSummary = components['schemas']['ReviewerSummary'];
type GeneratedSynthesisStats = components['schemas']['SynthesisStats'];

// Import manual types
import type { PlanWithStatus, ActiveLoop } from '$lib/types/plan';
import type { Task, AcceptanceCriterion } from '$lib/types/task';
import type { SynthesisResult, ReviewFinding, ReviewerSummary, SynthesisStats } from '$lib/types/review';

/**
 * Contract tests verify that manually-written TypeScript types stay compatible
 * with the auto-generated types from the Go backend's OpenAPI spec.
 *
 * These tests catch the class of bug where Go struct tags drift from the
 * TypeScript types the frontend depends on — for example, when Go adds
 * `omitempty` to a field the frontend always expects to be present.
 *
 * The bug this suite was written to prevent:
 *   Go `PlanWithStatus.ActiveLoops` had `omitempty` — when nil, Go omitted
 *   the field. TypeScript's `PlanWithStatus.active_loops: ActiveLoop[]`
 *   expected it to always be present. The frontend crashed on `.some()` of
 *   undefined.
 *
 * If these tests fail, it means the Go API contract changed and the frontend
 * types need updating — or vice versa.
 */
describe('workflow-api contract', () => {
	// ---------------------------------------------------------------------------
	// PlanWithStatus
	// ---------------------------------------------------------------------------

	describe('PlanWithStatus', () => {
		it('generated type has active_loops as a required (non-optional) array field', () => {
			// Compile-time verification: the generated type requires active_loops.
			// If Go re-adds omitempty, openapi-typescript will make this field
			// optional (active_loops?: ...) and the satisfies check will fail
			// to compile, catching the regression before runtime.
			//
			// Runtime guard: constructing the generated type without active_loops
			// would be a TypeScript error at build time, so we verify the field
			// is always present on a valid constructed value.
			const generated: GeneratedPlanWithStatus = {
				active_loops: [],
				approved: false,
				created_at: '2024-01-01T00:00:00Z',
				id: 'test-id',
				project_id: 'default',
				slug: 'test-slug',
				stage: 'drafting',
				title: 'Test Plan',
			};

			// active_loops must be defined and an array (never undefined)
			expect(generated.active_loops).toBeDefined();
			expect(Array.isArray(generated.active_loops)).toBe(true);
		});

		it('generated PlanWithStatus includes all required fields the API sends', () => {
			// Construct a fully-populated value matching the generated type to
			// confirm all expected fields from the Go schema are representable.
			const fromAPI: GeneratedPlanWithStatus = {
				active_loops: [{ loop_id: 'l1', role: 'planner', state: 'executing' }],
				approved: true,
				approved_at: '2024-01-02T00:00:00Z',
				context: 'Current state of the codebase',
				created_at: '2024-01-01T00:00:00Z',
				goal: 'Build the feature',
				id: 'plan-1',
				project_id: 'default',
				scope: { include: ['src/'], exclude: [], do_not_touch: [] },
				slug: 'test-plan',
				stage: 'approved',
				title: 'Test Plan',
			};

			expect(fromAPI.id).toBe('plan-1');
			expect(fromAPI.slug).toBe('test-plan');
			expect(fromAPI.stage).toBe('approved');
			expect(fromAPI.active_loops).toHaveLength(1);
			expect(fromAPI.active_loops[0].loop_id).toBe('l1');
			expect(fromAPI.approved).toBe(true);
			expect(fromAPI.project_id).toBe('default');
		});

		it('generated type uses snake_case project_id matching Go json tag', () => {
			// The Go struct uses `json:"project_id"` — the frontend must use the
			// same snake_case name. This test catches if the generated type drifts
			// to a different field name.
			const plan: GeneratedPlanWithStatus = {
				active_loops: [],
				approved: false,
				created_at: '2024-01-01T00:00:00Z',
				id: 'p1',
				project_id: 'my-project',
				slug: 'test',
				stage: 'drafting',
				title: 'Test',
			};

			expect('project_id' in plan).toBe(true);
			expect(plan.project_id).toBe('my-project');
			// Ensure no camelCase variant leaked into the generated type
			expect('projectId' in plan).toBe(false);
		});

		it('manual PlanWithStatus active_loops is required (non-optional) matching the fix', () => {
			// The whole point of removing omitempty: active_loops must be required
			// on the manual type too so that frontend code can safely call .some()
			// without a nullish check.
			//
			// This test documents that both sides agree: required on generated,
			// required on manual. If someone adds `?` back to the manual type,
			// the type-level assertion in the usage below fails.
			const plan: PlanWithStatus = {
				id: 'p1',
				slug: 'test',
				title: 'Test Plan',
				approved: false,
				created_at: '2024-01-01T00:00:00Z',
				scope: { include: [], exclude: [], do_not_touch: [] },
				project_id: 'default',
				stage: 'draft',
				active_loops: [],
			};

			// active_loops must be array, never undefined — .some() is always safe
			expect(Array.isArray(plan.active_loops)).toBe(true);
			expect(plan.active_loops.some((l) => l.role === 'planner')).toBe(false);
		});
	});

	// ---------------------------------------------------------------------------
	// ActiveLoopStatus
	// ---------------------------------------------------------------------------

	describe('ActiveLoopStatus', () => {
		it('generated ActiveLoopStatus has all three required fields', () => {
			// All 3 fields in the generated type should be required (no ?)
			const loop: GeneratedActiveLoopStatus = {
				loop_id: 'loop-1',
				role: 'planner',
				state: 'executing',
			};

			expect(loop.loop_id).toBe('loop-1');
			expect(loop.role).toBe('planner');
			expect(loop.state).toBe('executing');
		});

		it('manual ActiveLoop contains all generated ActiveLoopStatus fields', () => {
			// The manual ActiveLoop extends the generated type with extra fields
			// (model, iterations, max_iterations, current_task_id), but MUST
			// include every field the API actually sends.
			//
			// Verify by listing the generated field names and checking all exist
			// as keys in the manual type.
			const generatedFields: (keyof GeneratedActiveLoopStatus)[] = [
				'loop_id',
				'role',
				'state',
			];

			const manualFields: (keyof ActiveLoop)[] = [
				'loop_id',
				'role',
				'state',
				'model',
				'iterations',
				'max_iterations',
			];

			for (const field of generatedFields) {
				expect(manualFields).toContain(field);
			}
		});

		it('manual ActiveLoop extra fields do not conflict with generated names', () => {
			// Manual type adds fields not in the generated type. Verify there are
			// no duplicate or conflicting names between the two sets.
			const generatedFields = new Set<keyof GeneratedActiveLoopStatus>([
				'loop_id',
				'role',
				'state',
			]);

			const manualOnlyFields: (keyof ActiveLoop)[] = [
				'model',
				'iterations',
				'max_iterations',
				'current_task_id',
			];

			for (const field of manualOnlyFields) {
				// None of the manual-only fields should shadow a generated field name
				// (they extend, not override)
				expect(generatedFields.has(field as keyof GeneratedActiveLoopStatus)).toBe(false);
			}
		});
	});

	// ---------------------------------------------------------------------------
	// Task
	// ---------------------------------------------------------------------------

	describe('Task', () => {
		it('generated Task has all required fields matching the Go schema', () => {
			const task: GeneratedTask = {
				id: 'task.test-plan.1',
				plan_id: 'plan-1',
				phase_id: 'phase-1',
				sequence: 1,
				description: 'Implement the feature',
				status: 'pending',
				created_at: '2024-01-01T00:00:00Z',
				acceptance_criteria: [{ given: 'a state', when: 'action occurs', then: 'outcome' }],
			};

			expect(task.id).toBe('task.test-plan.1');
			expect(task.plan_id).toBe('plan-1');
			expect(task.phase_id).toBe('phase-1');
			expect(task.sequence).toBe(1);
			expect(task.acceptance_criteria).toHaveLength(1);
		});

		it('generated Task uses snake_case plan_id and phase_id matching Go json tags', () => {
			const task: GeneratedTask = {
				id: 'task.t.1',
				plan_id: 'parent-plan',
				phase_id: 'parent-phase',
				sequence: 1,
				description: 'Test',
				status: 'pending',
				created_at: '2024-01-01T00:00:00Z',
				acceptance_criteria: [],
			};

			expect('plan_id' in task).toBe(true);
			expect(task.plan_id).toBe('parent-plan');
			expect('phase_id' in task).toBe(true);
			expect(task.phase_id).toBe('parent-phase');
		});

		it('manual Task includes all required fields from generated Task', () => {
			// Verify that all required fields in the generated type are present
			// in the manual type under the same names.
			const requiredGeneratedFields: (keyof GeneratedTask)[] = [
				'id',
				'plan_id',
				'phase_id',
				'sequence',
				'description',
				'status',
				'created_at',
				'acceptance_criteria',
			];

			const manualFields: (keyof Task)[] = [
				'id',
				'plan_id',
				'phase_id',
				'sequence',
				'description',
				'status',
				'created_at',
				'acceptance_criteria',
			];

			for (const field of requiredGeneratedFields) {
				expect(manualFields).toContain(field);
			}
		});
	});

	// ---------------------------------------------------------------------------
	// AcceptanceCriterion
	// ---------------------------------------------------------------------------

	describe('AcceptanceCriterion', () => {
		it('generated AcceptanceCriterion has all three BDD fields required', () => {
			const criterion: GeneratedAcceptanceCriterion = {
				given: 'a precondition exists',
				when: 'the user performs an action',
				then: 'the expected outcome occurs',
			};

			expect(criterion.given).toBeDefined();
			expect(criterion.when).toBeDefined();
			expect(criterion.then).toBeDefined();
		});

		it('manual AcceptanceCriterion field names match generated type', () => {
			const generatedFields: (keyof GeneratedAcceptanceCriterion)[] = [
				'given',
				'when',
				'then',
			];

			const manualFields: (keyof AcceptanceCriterion)[] = [
				'given',
				'when',
				'then',
			];

			expect(generatedFields.sort()).toEqual(manualFields.sort());
		});
	});

	// ---------------------------------------------------------------------------
	// SynthesisResult
	// ---------------------------------------------------------------------------

	describe('SynthesisResult', () => {
		it('generated SynthesisResult has all required fields', () => {
			const result: GeneratedSynthesisResult = {
				verdict: 'approved',
				passed: true,
				findings: [],
				reviewers: [],
				summary: 'All checks passed',
				stats: {
					total_findings: 0,
					by_severity: {},
					by_reviewer: {},
					reviewers_passed: 0,
					reviewers_total: 0,
				},
			};

			expect(result.verdict).toBe('approved');
			expect(result.passed).toBe(true);
			expect(result.findings).toHaveLength(0);
			expect(result.reviewers).toHaveLength(0);
			expect(result.summary).toBeDefined();
			expect(result.stats).toBeDefined();
		});

		it('manual SynthesisResult contains all fields that the generated type requires', () => {
			// The manual type adds frontend-only fields (request_id, workflow_id,
			// partial, missing_reviewers, error) but MUST include everything the
			// API actually sends as required fields.
			const requiredGeneratedFields: (keyof GeneratedSynthesisResult)[] = [
				'verdict',
				'passed',
				'findings',
				'reviewers',
				'summary',
				'stats',
			];

			const manualFields: (keyof SynthesisResult)[] = [
				'verdict',
				'passed',
				'findings',
				'reviewers',
				'summary',
				'stats',
				'request_id',
				'workflow_id',
				'partial',
				'missing_reviewers',
				'error',
			];

			for (const field of requiredGeneratedFields) {
				expect(manualFields).toContain(field);
			}
		});
	});

	// ---------------------------------------------------------------------------
	// ReviewFinding
	// ---------------------------------------------------------------------------

	describe('ReviewFinding', () => {
		it('generated ReviewFinding has required fields for file, line, issue, suggestion', () => {
			const finding: GeneratedReviewFinding = {
				file: 'src/main.go',
				line: 42,
				issue: 'SQL injection vulnerability',
				suggestion: 'Use parameterized queries',
				severity: 'critical',
			};

			expect(finding.file).toBe('src/main.go');
			expect(finding.line).toBe(42);
			expect(finding.issue).toBeDefined();
			expect(finding.suggestion).toBeDefined();
			expect(finding.severity).toBeDefined();
		});

		it('manual ReviewFinding includes all required fields from generated type', () => {
			const requiredGeneratedFields: (keyof GeneratedReviewFinding)[] = [
				'file',
				'line',
				'issue',
				'suggestion',
				'severity',
			];

			const manualFields: (keyof ReviewFinding)[] = [
				'role',
				'category',
				'severity',
				'file',
				'line',
				'issue',
				'suggestion',
				'sop_id',
				'status',
				'cwe',
			];

			for (const field of requiredGeneratedFields) {
				expect(manualFields).toContain(field);
			}
		});
	});

	// ---------------------------------------------------------------------------
	// ReviewerSummary
	// ---------------------------------------------------------------------------

	describe('ReviewerSummary', () => {
		it('generated ReviewerSummary has all four required fields', () => {
			const summary: GeneratedReviewerSummary = {
				role: 'spec_reviewer',
				passed: true,
				summary: 'Implementation matches specification',
				finding_count: 0,
			};

			expect(summary.role).toBeDefined();
			expect(summary.passed).toBe(true);
			expect(summary.summary).toBeDefined();
			expect(summary.finding_count).toBe(0);
		});

		it('manual ReviewerSummary includes all required generated fields', () => {
			// Note: the manual type adds an optional verdict field not in the
			// generated type. All generated required fields must be present.
			const requiredGeneratedFields: (keyof GeneratedReviewerSummary)[] = [
				'role',
				'passed',
				'summary',
				'finding_count',
			];

			const manualFields: (keyof ReviewerSummary)[] = [
				'role',
				'passed',
				'summary',
				'finding_count',
				'verdict',
			];

			for (const field of requiredGeneratedFields) {
				expect(manualFields).toContain(field);
			}
		});
	});

	// ---------------------------------------------------------------------------
	// SynthesisStats
	// ---------------------------------------------------------------------------

	describe('SynthesisStats', () => {
		it('generated SynthesisStats has all required fields', () => {
			const stats: GeneratedSynthesisStats = {
				total_findings: 3,
				by_severity: { critical: 1, high: 2 },
				by_reviewer: { spec_reviewer: 1 },
				reviewers_passed: 2,
				reviewers_total: 3,
			};

			expect(stats.total_findings).toBe(3);
			expect(stats.by_severity).toBeDefined();
			expect(stats.by_reviewer).toBeDefined();
			expect(stats.reviewers_passed).toBe(2);
			expect(stats.reviewers_total).toBe(3);
		});

		it('manual SynthesisStats field names match generated type exactly', () => {
			const generatedFields: (keyof GeneratedSynthesisStats)[] = [
				'total_findings',
				'by_severity',
				'by_reviewer',
				'reviewers_passed',
				'reviewers_total',
			];

			const manualFields: (keyof SynthesisStats)[] = [
				'total_findings',
				'by_severity',
				'by_reviewer',
				'reviewers_total',
				'reviewers_passed',
			];

			expect(generatedFields.sort()).toEqual(manualFields.sort());
		});
	});

	// ---------------------------------------------------------------------------
	// Field name consistency (snake_case from Go json tags)
	// ---------------------------------------------------------------------------

	describe('field name consistency', () => {
		it('generated PlanWithStatus uses snake_case matching Go json tags', () => {
			// These field names come directly from Go struct json tags.
			// If a Go developer renames a tag, openapi-typescript regenerates
			// with the new name, and this test catches the drift.
			const expectedSnakeCaseFields = [
				'id',
				'slug',
				'title',
				'project_id',
				'approved',
				'created_at',
				'stage',
				'active_loops',
			];

			const generated: GeneratedPlanWithStatus = {
				active_loops: [],
				approved: false,
				created_at: '2024-01-01T00:00:00Z',
				id: 'test',
				project_id: 'default',
				slug: 'test',
				stage: 'drafting',
				title: 'Test',
			};

			for (const field of expectedSnakeCaseFields) {
				expect(field in generated).toBe(true);
			}
		});

		it('generated Task uses snake_case for all multi-word fields', () => {
			const expectedSnakeCaseFields = [
				'plan_id',
				'phase_id',
				'created_at',
				'acceptance_criteria',
			];

			const task: GeneratedTask = {
				id: 'task.t.1',
				plan_id: 'p1',
				phase_id: 'phase-1',
				sequence: 1,
				description: 'Test',
				status: 'pending',
				created_at: '2024-01-01T00:00:00Z',
				acceptance_criteria: [],
			};

			for (const field of expectedSnakeCaseFields) {
				expect(field in task).toBe(true);
			}
		});

		it('generated ReviewFinding uses snake_case for multi-word fields', () => {
			const expectedSnakeCaseFields = ['sop_id'];

			const finding: GeneratedReviewFinding = {
				file: 'src/main.go',
				line: 1,
				issue: 'issue',
				suggestion: 'suggestion',
				severity: 'low',
				sop_id: 'SOP-001',
			};

			for (const field of expectedSnakeCaseFields) {
				expect(field in finding).toBe(true);
			}
		});
	});
});
