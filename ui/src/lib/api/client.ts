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
import type { SynthesisResult } from '$lib/types/review';
import type { ContextBuildResponse } from '$lib/types/context';
import type { Trajectory } from '$lib/types/trajectory';

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
				'/workflow-api/plans',
				{ method: 'POST', body: params }
			),

		/** List all plans (drafts and approved) */
		list: (params?: { approved?: boolean; stage?: string }) =>
			request<PlanWithStatus[]>(`/workflow-api/plans${toQueryString(params)}`),

		/** Get a single plan by slug */
		get: (slug: string) => request<PlanWithStatus>(`/workflow-api/plans/${slug}`),

		/** Update a plan (goal, context, scope) */
		update: (slug: string, data: UpdatePlanRequest) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}`, { method: 'PATCH', body: data }),

		/** Delete or archive a plan */
		delete: (slug: string, archive?: boolean) =>
			request<void>(`/workflow-api/plans/${slug}${archive ? '?archive=true' : ''}`, {
				method: 'DELETE'
			}),

		/** Get tasks for a plan */
		getTasks: (slug: string) => request<Task[]>(`/workflow-api/plans/${slug}/tasks`),

		/** Approve a draft plan */
		promote: (slug: string) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}/promote`, { method: 'POST' }),

		/** Generate tasks for an approved plan */
		generateTasks: (slug: string) =>
			request<Task[]>(`/workflow-api/plans/${slug}/tasks/generate`, { method: 'POST' }),

		/** Start executing tasks for a plan */
		execute: (slug: string) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}/execute`, { method: 'POST' }),

		/** Get review synthesis result for a plan */
		getReviews: (slug: string) => request<SynthesisResult>(`/workflow-api/plans/${slug}/reviews`),

		/** Batch approve all pending tasks */
		approveTasks: (slug: string) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}/tasks/approve`, { method: 'POST' })
	},

	tasks: {
		/** Get a single task by ID */
		get: (slug: string, taskId: string) =>
			request<Task>(`/workflow-api/plans/${slug}/tasks/${taskId}`),

		/** Create a new task manually */
		create: (slug: string, data: CreateTaskRequest) =>
			request<Task>(`/workflow-api/plans/${slug}/tasks`, { method: 'POST', body: data }),

		/** Update an existing task */
		update: (slug: string, taskId: string, data: UpdateTaskRequest) =>
			request<Task>(`/workflow-api/plans/${slug}/tasks/${taskId}`, { method: 'PATCH', body: data }),

		/** Delete a task */
		delete: (slug: string, taskId: string) =>
			request<void>(`/workflow-api/plans/${slug}/tasks/${taskId}`, { method: 'DELETE' }),

		/** Approve a single task */
		approve: (slug: string, taskId: string, approvedBy?: string) =>
			request<Task>(`/workflow-api/plans/${slug}/tasks/${taskId}/approve`, {
				method: 'POST',
				body: { approved_by: approvedBy }
			}),

		/** Reject a single task with reason */
		reject: (slug: string, taskId: string, reason: string, rejectedBy?: string) =>
			request<Task>(`/workflow-api/plans/${slug}/tasks/${taskId}/reject`, {
				method: 'POST',
				body: { reason, rejected_by: rejectedBy }
			})
	},

	context: {
		/** Get context build response by request ID */
		get: (requestId: string) =>
			request<ContextBuildResponse>(`/context-builder/responses/${requestId}`)
	},

	trajectory: {
		/** Get trajectory for a loop */
		getByLoop: (loopId: string, format?: 'summary' | 'json') =>
			request<Trajectory>(`/trajectory-api/loops/${loopId}?format=${format ?? 'json'}`),

		/** Get trajectory for a trace */
		getByTrace: (traceId: string, format?: 'summary' | 'json') =>
			request<Trajectory>(`/trajectory-api/traces/${traceId}?format=${format ?? 'json'}`)
	}
};
