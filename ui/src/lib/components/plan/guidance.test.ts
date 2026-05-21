import { describe, it, expect } from 'vitest';
import { deriveGuidance } from './guidance';
import type { PlanStage } from '$lib/types/plan';

/**
 * Pins the bug #7.7 fix: implementing/executing/legacy stages return null
 * (no dead hint). If someone re-adds a hint that points at a per-requirement
 * inline timeline that doesn't exist, this test fails.
 */
describe('deriveGuidance', () => {
	it('reviewed unapproved plan shows review + approve hint', () => {
		// Once the planner LLM and plan-reviewer have both finished, the plan
		// is ready for the human to click Create Requirements.
		const g = deriveGuidance(false, 'reviewed', 0);
		expect(g?.message).toMatch(/review the plan details/i);
		expect(g?.showApprove).toBe(true);
	});

	it.each([
		['drafting', /composing/i],
		['drafted', /waiting for plan reviewer/i],
		['reviewing_draft', /reviewer is evaluating/i]
	] as [PlanStage, RegExp][])(
		'stage=%s hides approve button and shows loading hint',
		(stage, messagePattern) => {
			const g = deriveGuidance(false, stage, 0);
			expect(g?.showApprove).toBe(false);
			expect(g?.isLoading).toBe(true);
			expect(g?.message).toMatch(messagePattern);
		}
	);

	it('approved + no requirements yet shows generation spinner', () => {
		const g = deriveGuidance(true, 'approved', 0);
		expect(g?.isLoading).toBe(true);
	});

	it('requirements_generated shows "generating scenarios" spinner', () => {
		const g = deriveGuidance(true, 'requirements_generated', 3);
		expect(g?.isLoading).toBe(true);
		expect(g?.message).toMatch(/generating scenarios/i);
	});

	it('scenarios_generated prompts review + approve', () => {
		const g = deriveGuidance(true, 'scenarios_generated', 3);
		expect(g?.message).toMatch(/review the requirements and scenarios/i);
	});

	it('ready_for_execution prompts Execute click', () => {
		const g = deriveGuidance(true, 'ready_for_execution', 3);
		expect(g?.message).toMatch(/click execute/i);
	});

	it('complete acknowledges completion', () => {
		const g = deriveGuidance(true, 'complete', 3);
		expect(g?.message).toMatch(/complete/i);
	});

	// Bug #7.7 — the key regression pins. Previously these returned hints
	// like "Select a requirement to view progress" pointing at an inline
	// timeline that was never built. `implementing`/`executing` still
	// return null because their progress is surfaced by ExecutionTimeline
	// and AgentPipelineView, not by the top-level guidance panel.
	it.each([
		'implementing',
		'executing',
		'tasks',
		'phases_generated'
	] as PlanStage[])('stage=%s returns null (handled by other UI)', (stage) => {
		expect(deriveGuidance(true, stage, 3)).toBeNull();
	});

	// 2026-05-21: explicit isLoading entries for every backend-emitted
	// generator/reviewer stage. Previously these fell through to indirect
	// heuristics that missed real cases (e.g. generating_requirements
	// before requirementCount propagated). The in-progress panel above
	// PlanDetail keys on isLoading, so missing entries here = blank UI
	// during long LLM phases.
	it.each([
		['generating_requirements', /requirements/i],
		['generating_architecture', /architecture/i],
		['generating_scenarios', /scenarios/i],
		['reviewing_scenarios', /requirements and scenarios/i],
		['reviewing_qa', /qa/i],
		['reviewing_rollup', /rollup/i]
	] as [PlanStage, RegExp][])(
		'stage=%s shows loading hint with isLoading=true',
		(stage, messagePattern) => {
			const g = deriveGuidance(true, stage, 3);
			expect(g?.isLoading).toBe(true);
			expect(g?.showApprove).toBe(false);
			expect(g?.message).toMatch(messagePattern);
		}
	);

	it('never returns the deleted "select a requirement" copy at any stage', () => {
		// Exhaustive pass: scan every known stage and confirm the hint copy
		// that pointed at the non-existent inline timeline doesn't surface.
		const stages: PlanStage[] = [
			'draft',
			'drafting',
			'ready_for_approval',
			'reviewed',
			'needs_changes',
			'planning',
			'approved',
			'rejected',
			'requirements_generated',
			'scenarios_generated',
			'scenarios_reviewed',
			'ready_for_execution',
			'phases_generated',
			'phases_approved',
			'tasks_generated',
			'tasks_approved',
			'tasks',
			'implementing',
			'executing',
			'reviewing_rollup',
			'complete',
			'archived',
			'failed'
		];
		for (const stage of stages) {
			for (const approved of [true, false]) {
				for (const count of [0, 3]) {
					const msg = deriveGuidance(approved, stage, count)?.message ?? '';
					expect(msg.toLowerCase()).not.toContain('select a requirement');
				}
			}
		}
	});
});
