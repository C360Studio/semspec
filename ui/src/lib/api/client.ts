import { mockRequest } from './mock';
import { graphqlRequest } from './graphql';
import {
	transformEntity,
	transformRelationships,
	transformEntityCounts,
	type RawEntity,
	type RawRelationship,
	type EntityIdHierarchy
} from './transforms';
import type {
	Loop,
	MessageResponse,
	SignalResponse,
	Entity,
	EntityWithRelationships,
	EntityListParams
} from '$lib/types';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task, AcceptanceCriterion, TaskType } from '$lib/types/task';
import type { Phase, PhaseAgentConfig } from '$lib/types/phase';
import type { SynthesisResult } from '$lib/types/review';
import type { ContextBuildResponse } from '$lib/types/context';
import type { Trajectory } from '$lib/types/trajectory';
import type { Requirement } from '$lib/types/requirement';
import type { Scenario } from '$lib/types/scenario';
import type { ChangeProposal } from '$lib/types/change-proposal';

const BASE_URL = import.meta.env.VITE_API_URL || '';
const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true';

interface RequestOptions {
	method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
	body?: unknown;
	headers?: Record<string, string>;
}

/** Request body for creating a task manually */
export interface CreateTaskRequest {
	description: string;
	type?: TaskType;
	acceptance_criteria?: AcceptanceCriterion[];
	files?: string[];
	depends_on?: string[];
}

/** Request body for updating a task */
export interface UpdateTaskRequest {
	description?: string;
	type?: TaskType;
	acceptance_criteria?: AcceptanceCriterion[];
	files?: string[];
	depends_on?: string[];
	sequence?: number;
}

/** Request body for approving a task */
export interface ApproveTaskRequest {
	approved_by?: string;
}

/** Request body for rejecting a task */
export interface RejectTaskRequest {
	reason: string;
	rejected_by?: string;
}

/** Request body for updating a plan */
export interface UpdatePlanRequest {
	title?: string;
	goal?: string;
	context?: string;
	scope?: {
		include_patterns?: string[];
		exclude_patterns?: string[];
	};
}

/** Request body for creating a phase */
export interface CreatePhaseRequest {
	name: string;
	description?: string;
	depends_on?: string[];
	agent_config?: PhaseAgentConfig;
	requires_approval?: boolean;
}

/** Request body for updating a phase */
export interface UpdatePhaseRequest {
	name?: string;
	description?: string;
	depends_on?: string[];
	agent_config?: PhaseAgentConfig;
	requires_approval?: boolean;
	sequence?: number;
}

/** Request body for rejecting a phase */
export interface RejectPhaseRequest {
	reason: string;
	rejected_by?: string;
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
	if (USE_MOCKS) {
		return mockRequest<T>(path, options);
	}

	const { method = 'GET', body, headers = {} } = options;

	const response = await fetch(`${BASE_URL}${path}`, {
		method,
		headers: {
			'Content-Type': 'application/json',
			...headers
		},
		body: body ? JSON.stringify(body) : undefined
	});

	if (!response.ok) {
		const error = await response.json().catch(() => ({ message: response.statusText }));
		throw new Error(error.message || `Request failed: ${response.status}`);
	}

	return response.json();
}

function toQueryString(params?: Record<string, unknown>): string {
	if (!params) return '';
	const entries = Object.entries(params).filter(([, v]) => v !== undefined);
	if (entries.length === 0) return '';
	return '?' + new URLSearchParams(entries.map(([k, v]) => [k, String(v)])).toString();
}

export const api = {
	router: {
		getLoops: (params?: { user_id?: string; state?: string }) =>
			request<Loop[]>(`/agentic-dispatch/loops${toQueryString(params)}`),

		getLoop: (id: string) => request<Loop>(`/agentic-dispatch/loops/${id}`),

		sendSignal: (loopId: string, type: string, reason?: string) =>
			request<SignalResponse>(`/agentic-dispatch/loops/${loopId}/signal`, {
				method: 'POST',
				body: { type, reason }
			}),

		sendMessage: (content: string) =>
			request<MessageResponse>('/agentic-dispatch/message', {
				method: 'POST',
				body: { content }
			})
	},

	system: {
		getHealth: () => request('/agentic-dispatch/health')
	},

	entities: {
		list: async (params?: EntityListParams): Promise<Entity[]> => {
			const prefix = params?.type ? `${params.type}.` : '';
			const limit = params?.limit || 100;

			const result = await graphqlRequest<{ entitiesByPrefix: RawEntity[] }>(
				`
				query($prefix: String!, $limit: Int) {
					entitiesByPrefix(prefix: $prefix, limit: $limit) {
						id
						triples { subject predicate object }
					}
				}
			`,
				{ prefix, limit }
			);

			let entities = result.entitiesByPrefix.map(transformEntity);

			// Apply client-side search filter if query provided
			if (params?.query) {
				const q = params.query.toLowerCase();
				entities = entities.filter(
					(e) =>
						e.name.toLowerCase().includes(q) ||
						e.id.toLowerCase().includes(q) ||
						JSON.stringify(e.predicates).toLowerCase().includes(q)
				);
			}

			return entities;
		},

		get: async (id: string): Promise<EntityWithRelationships> => {
			const result = await graphqlRequest<{
				entity: RawEntity;
				relationships: RawRelationship[];
			}>(
				`
				query($id: String!) {
					entity(id: $id) {
						id
						triples { subject predicate object }
					}
					relationships(entityId: $id) {
						from
						to
						predicate
						direction
					}
				}
			`,
				{ id }
			);

			if (!result.entity) {
				throw new Error('Entity not found');
			}

			return {
				...transformEntity(result.entity),
				relationships: transformRelationships(result.relationships || [])
			};
		},

		relationships: async (id: string) => {
			const result = await graphqlRequest<{ relationships: RawRelationship[] }>(
				`
				query($id: String!) {
					relationships(entityId: $id) {
						from
						to
						predicate
						direction
					}
				}
			`,
				{ id }
			);

			return transformRelationships(result.relationships || []);
		},

		count: async (): Promise<{ total: number; byType: Record<string, number> }> => {
			const result = await graphqlRequest<{ entityIdHierarchy: EntityIdHierarchy }>(
				`
				query {
					entityIdHierarchy(prefix: "") {
						children { name count }
						totalEntities
					}
				}
			`
			);

			return transformEntityCounts(result.entityIdHierarchy);
		}
	},

	plans: {
		/** Create a new plan from a description */
		create: (params: { description: string }) =>
			request<{ slug: string; request_id: string; trace_id: string; message: string }>(
				'/plan-manager/plans',
				{ method: 'POST', body: params }
			),

		/** List all plans (drafts and approved) */
		list: (params?: { approved?: boolean; stage?: string }) =>
			request<PlanWithStatus[]>(`/plan-manager/plans${toQueryString(params)}`),

		/** Get a single plan by slug */
		get: (slug: string) => request<PlanWithStatus>(`/plan-manager/plans/${slug}`),

		/** Update a plan (goal, context, scope) */
		update: (slug: string, data: UpdatePlanRequest) =>
			request<PlanWithStatus>(`/plan-manager/plans/${slug}`, { method: 'PATCH', body: data }),

		/** Delete or archive a plan */
		delete: (slug: string, archive?: boolean) =>
			request<void>(`/plan-manager/plans/${slug}${archive ? '?archive=true' : ''}`, {
				method: 'DELETE'
			}),

		/** Get tasks for a plan */
		getTasks: (slug: string) => request<Task[]>(`/plan-manager/plans/${slug}/tasks`),

		/** Approve a draft plan */
		promote: (slug: string) =>
			request<PlanWithStatus>(`/plan-manager/plans/${slug}/promote`, { method: 'POST' }),

		/** Generate tasks for an approved plan */
		generateTasks: (slug: string) =>
			request<Task[]>(`/plan-manager/plans/${slug}/tasks/generate`, { method: 'POST' }),

		/** Start executing tasks for a plan */
		execute: (slug: string) =>
			request<PlanWithStatus>(`/plan-manager/plans/${slug}/execute`, { method: 'POST' }),

		/** Get review synthesis result for a plan */
		getReviews: (slug: string) => request<SynthesisResult>(`/plan-manager/plans/${slug}/reviews`),

		/** Batch approve all pending tasks */
		approveTasks: (slug: string) =>
			request<PlanWithStatus>(`/plan-manager/plans/${slug}/tasks/approve`, { method: 'POST' })
	},

	phases: {
		/** List all phases for a plan */
		list: (slug: string) => request<Phase[]>(`/plan-manager/plans/${slug}/phases`),

		/** Get a single phase by ID */
		get: (slug: string, phaseId: string) =>
			request<Phase>(`/plan-manager/plans/${slug}/phases/${phaseId}`),

		/** Create a new phase */
		create: (slug: string, data: CreatePhaseRequest) =>
			request<Phase>(`/plan-manager/plans/${slug}/phases`, { method: 'POST', body: data }),

		/** Update an existing phase */
		update: (slug: string, phaseId: string, data: UpdatePhaseRequest) =>
			request<Phase>(`/plan-manager/plans/${slug}/phases/${phaseId}`, {
				method: 'PATCH',
				body: data
			}),

		/** Delete a phase */
		delete: (slug: string, phaseId: string) =>
			request<void>(`/plan-manager/plans/${slug}/phases/${phaseId}`, { method: 'DELETE' }),

		/** Approve a phase for execution */
		approve: (slug: string, phaseId: string, approvedBy?: string) =>
			request<Phase>(`/plan-manager/plans/${slug}/phases/${phaseId}/approve`, {
				method: 'POST',
				body: { approved_by: approvedBy }
			}),

		/** Reject a phase with reason */
		reject: (slug: string, phaseId: string, reason: string, rejectedBy?: string) =>
			request<Phase>(`/plan-manager/plans/${slug}/phases/${phaseId}/reject`, {
				method: 'POST',
				body: { reason, rejected_by: rejectedBy }
			}),

		/** Reorder phases */
		reorder: (slug: string, phaseIds: string[]) =>
			request<Phase[]>(`/plan-manager/plans/${slug}/phases/reorder`, {
				method: 'POST',
				body: { phase_ids: phaseIds }
			}),

		/** Generate phases from plan */
		generate: (slug: string) =>
			request<Phase[]>(`/plan-manager/plans/${slug}/phases/generate`, { method: 'POST' }),

		/** Batch approve all pending phases */
		approveAll: (slug: string) =>
			request<PlanWithStatus>(`/plan-manager/plans/${slug}/phases/approve`, { method: 'POST' })
	},

	tasks: {
		/** Get a single task by ID */
		get: (slug: string, taskId: string) =>
			request<Task>(`/plan-manager/plans/${slug}/tasks/${taskId}`),

		/** List tasks for a specific phase */
		listByPhase: (slug: string, phaseId: string) =>
			request<Task[]>(`/plan-manager/plans/${slug}/phases/${phaseId}/tasks`),

		/** Create a new task manually */
		create: (slug: string, data: CreateTaskRequest & { phase_id?: string }) =>
			request<Task>(`/plan-manager/plans/${slug}/tasks`, { method: 'POST', body: data }),

		/** Update an existing task */
		update: (slug: string, taskId: string, data: UpdateTaskRequest & { phase_id?: string }) =>
			request<Task>(`/plan-manager/plans/${slug}/tasks/${taskId}`, { method: 'PATCH', body: data }),

		/** Delete a task */
		delete: (slug: string, taskId: string) =>
			request<void>(`/plan-manager/plans/${slug}/tasks/${taskId}`, { method: 'DELETE' }),

		/** Approve a single task */
		approve: (slug: string, taskId: string, approvedBy?: string) =>
			request<Task>(`/plan-manager/plans/${slug}/tasks/${taskId}/approve`, {
				method: 'POST',
				body: { approved_by: approvedBy }
			}),

		/** Reject a single task with reason */
		reject: (slug: string, taskId: string, reason: string, rejectedBy?: string) =>
			request<Task>(`/plan-manager/plans/${slug}/tasks/${taskId}/reject`, {
				method: 'POST',
				body: { reason, rejected_by: rejectedBy }
			})
	},

	requirements: {
		/** List all requirements for a plan */
		list: (slug: string) => request<Requirement[]>(`/plan-manager/plans/${slug}/requirements`),

		/** Get a single requirement by ID */
		get: (slug: string, reqId: string) =>
			request<Requirement>(`/plan-manager/plans/${slug}/requirements/${reqId}`),

		/** Create a new requirement */
		create: (slug: string, data: { title: string; description?: string }) =>
			request<Requirement>(`/plan-manager/plans/${slug}/requirements`, {
				method: 'POST',
				body: data
			}),

		/** Update an existing requirement */
		update: (slug: string, reqId: string, data: { title?: string; description?: string }) =>
			request<Requirement>(`/plan-manager/plans/${slug}/requirements/${reqId}`, {
				method: 'PATCH',
				body: data
			}),

		/** Delete a requirement */
		delete: (slug: string, reqId: string) =>
			request<void>(`/plan-manager/plans/${slug}/requirements/${reqId}`, { method: 'DELETE' }),

		/** Deprecate a requirement */
		deprecate: (slug: string, reqId: string) =>
			request<Requirement>(`/plan-manager/plans/${slug}/requirements/${reqId}/deprecate`, {
				method: 'POST'
			})
	},

	scenarios: {
		/** List all scenarios for a plan */
		list: (slug: string) => request<Scenario[]>(`/plan-manager/plans/${slug}/scenarios`),

		/** List scenarios for a specific requirement */
		listByRequirement: (slug: string, reqId: string) =>
			request<Scenario[]>(
				`/plan-manager/plans/${slug}/scenarios?requirement_id=${encodeURIComponent(reqId)}`
			),

		/** Get a single scenario by ID */
		get: (slug: string, scenarioId: string) =>
			request<Scenario>(`/plan-manager/plans/${slug}/scenarios/${scenarioId}`),

		/** Create a new scenario */
		create: (
			slug: string,
			data: { requirement_id: string; given: string; when: string; then: string[] }
		) =>
			request<Scenario>(`/plan-manager/plans/${slug}/scenarios`, { method: 'POST', body: data }),

		/** Update an existing scenario */
		update: (
			slug: string,
			scenarioId: string,
			data: { given?: string; when?: string; then?: string[] }
		) =>
			request<Scenario>(`/plan-manager/plans/${slug}/scenarios/${scenarioId}`, {
				method: 'PATCH',
				body: data
			}),

		/** Delete a scenario */
		delete: (slug: string, scenarioId: string) =>
			request<void>(`/plan-manager/plans/${slug}/scenarios/${scenarioId}`, { method: 'DELETE' })
	},

	changeProposals: {
		/** List all change proposals for a plan */
		list: (slug: string) =>
			request<ChangeProposal[]>(`/plan-manager/plans/${slug}/change-proposals`),

		/** Get a single change proposal by ID */
		get: (slug: string, proposalId: string) =>
			request<ChangeProposal>(`/plan-manager/plans/${slug}/change-proposals/${proposalId}`),

		/** Create a new change proposal */
		create: (
			slug: string,
			data: {
				title: string;
				rationale: string;
				proposed_by?: string;
				affected_requirement_ids: string[];
			}
		) =>
			request<ChangeProposal>(`/plan-manager/plans/${slug}/change-proposals`, {
				method: 'POST',
				body: data
			}),

		/** Submit a change proposal for review */
		submit: (slug: string, proposalId: string) =>
			request<ChangeProposal>(
				`/plan-manager/plans/${slug}/change-proposals/${proposalId}/submit`,
				{ method: 'POST' }
			),

		/** Accept a change proposal */
		accept: (slug: string, proposalId: string) =>
			request<ChangeProposal>(
				`/plan-manager/plans/${slug}/change-proposals/${proposalId}/accept`,
				{ method: 'POST' }
			),

		/** Reject a change proposal */
		reject: (slug: string, proposalId: string, reason?: string) =>
			request<ChangeProposal>(
				`/plan-manager/plans/${slug}/change-proposals/${proposalId}/reject`,
				{ method: 'POST', body: reason ? { reason } : {} }
			)
	},

	context: {
		/** Get context build response by request ID */
		get: (requestId: string) =>
			request<ContextBuildResponse>(`/context-builder/responses/${requestId}`)
	},

	trajectory: {
		/** Get trajectory for a loop */
		getByLoop: (loopId: string, format?: 'summary' | 'json') =>
			request<Trajectory>(`/agentic-loop/trajectories/${loopId}?format=${format ?? 'json'}`),

		/** Get trajectory for a trace */
		getByTrace: (traceId: string, format?: 'summary' | 'json') =>
			request<Trajectory>(`/agentic-loop/trajectories/trace/${traceId}?format=${format ?? 'json'}`)
	}
};
