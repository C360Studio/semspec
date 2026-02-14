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
import type { Task } from '$lib/types/task';
import type { SynthesisResult } from '$lib/types/review';
import type { ContextBuildResponse } from '$lib/types/context';

const BASE_URL = import.meta.env.VITE_API_URL || '';
const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true';

interface RequestOptions {
	method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
	body?: unknown;
	headers?: Record<string, string>;
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
		/** List all plans (explorations and committed) */
		list: (params?: { committed?: boolean; stage?: string }) =>
			request<PlanWithStatus[]>(`/workflow-api/plans${toQueryString(params)}`),

		/** Get a single plan by slug */
		get: (slug: string) => request<PlanWithStatus>(`/workflow-api/plans/${slug}`),

		/** Get tasks for a plan */
		getTasks: (slug: string) => request<Task[]>(`/workflow-api/plans/${slug}/tasks`),

		/** Promote an exploration to a committed plan */
		promote: (slug: string) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}/promote`, { method: 'POST' }),

		/** Generate tasks for a committed plan */
		generateTasks: (slug: string) =>
			request<Task[]>(`/workflow-api/plans/${slug}/tasks/generate`, { method: 'POST' }),

		/** Start executing tasks for a plan */
		execute: (slug: string) =>
			request<PlanWithStatus>(`/workflow-api/plans/${slug}/execute`, { method: 'POST' }),

		/** Get review synthesis result for a plan */
		getReviews: (slug: string) => request<SynthesisResult>(`/workflow-api/plans/${slug}/reviews`)
	},

	context: {
		/** Get context build response by request ID */
		get: (requestId: string) =>
			request<ContextBuildResponse>(`/context-builder/responses/${requestId}`)
	}
};
