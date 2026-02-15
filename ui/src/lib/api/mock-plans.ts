/**
 * Mock data for ADR-003 Plan + Tasks workflow model.
 */

import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';

/**
 * Sample plans including both drafts and approved plans.
 */
export const mockPlans: PlanWithStatus[] = [
	{
		id: 'plan.add-user-authentication',
		slug: 'add-user-authentication',
		title: 'Add User Authentication with JWT Tokens',
		approved: true,
		created_at: '2025-02-08T09:00:00Z',
		approved_at: '2025-02-08T12:00:00Z',
		goal: 'Implement JWT-based user authentication with registration, login, and protected routes.',
		context:
			'The application currently has no user authentication. Users can access all endpoints without any verification. This blocks several planned features that require user identity.',
		scope: {
			include: ['cmd/api/', 'internal/auth/', 'internal/user/'],
			exclude: ['internal/admin/'],
			do_not_touch: ['internal/core/']
		},
		projectId: 'default',
		stage: 'executing',
		github: {
			epic_number: 42,
			epic_url: 'https://github.com/org/repo/issues/42',
			repository: 'org/repo',
			task_issues: { '1': 43, '2': 44, '3': 45 }
		},
		active_loops: [
			{
				loop_id: 'loop_abc123',
				role: 'developer',
				model: 'qwen',
				state: 'executing',
				iterations: 2,
				max_iterations: 3,
				current_task_id: 'task.add-user-authentication.4'
			}
		],
		task_stats: { total: 7, completed: 3, failed: 0, in_progress: 1 }
	},
	{
		id: 'plan.refactor-database-layer',
		slug: 'refactor-database-layer',
		title: 'Refactor Database Layer for Connection Pooling',
		approved: true,
		created_at: '2025-02-10T14:00:00Z',
		approved_at: '2025-02-10T16:00:00Z',
		goal: 'Implement connection pooling to improve database performance under load.',
		context:
			'Current implementation creates a new connection per request. Under load testing, we see connection exhaustion errors.',
		scope: {
			include: ['internal/database/', 'internal/repository/'],
			exclude: [],
			do_not_touch: ['migrations/']
		},
		projectId: 'default',
		stage: 'tasks',
		active_loops: [],
		task_stats: { total: 5, completed: 0, failed: 0, in_progress: 0 }
	},
	{
		id: 'plan.fix-login-redirect',
		slug: 'fix-login-redirect',
		title: 'Fix Login Redirect Loop on Session Expiry',
		approved: false,
		created_at: '2025-02-11T08:00:00Z',
		goal: 'Fix the infinite redirect loop when sessions expire during active use.',
		context:
			'Users report being stuck in a redirect loop when their session expires while using the app.',
		scope: {
			include: ['internal/auth/', 'web/'],
			exclude: [],
			do_not_touch: []
		},
		projectId: 'default',
		stage: 'draft',
		active_loops: [
			{
				loop_id: 'loop_def456',
				role: 'planner',
				model: 'claude',
				state: 'executing',
				iterations: 1,
				max_iterations: 5
			}
		],
		task_stats: undefined
	},
	{
		id: 'plan.add-dark-mode',
		slug: 'add-dark-mode',
		title: 'Add Dark Mode Support',
		approved: false,
		created_at: '2025-02-11T10:00:00Z',
		goal: 'Add system and user-preference dark mode support.',
		context: 'Users have requested dark mode. Many use the app at night.',
		scope: {
			include: ['web/styles/', 'web/components/'],
			exclude: [],
			do_not_touch: []
		},
		projectId: 'default',
		stage: 'draft',
		active_loops: [],
		task_stats: undefined
	}
];

/**
 * Sample tasks with BDD acceptance criteria.
 */
export const mockTasks: Record<string, Task[]> = {
	'add-user-authentication': [
		{
			id: 'task.add-user-authentication.1',
			plan_id: 'plan.add-user-authentication',
			sequence: 1,
			description: 'Create user model and database migration',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'A clean database',
					when: 'The migration runs',
					then: 'A users table exists with id, email, password_hash, created_at columns'
				},
				{
					given: 'The users table exists',
					when: 'A user record is inserted',
					then: 'The id is auto-generated as UUID'
				}
			],
			files: ['internal/user/model.go', 'migrations/002_add_users.sql'],
			status: 'completed',
			created_at: '2025-02-08T13:00:00Z',
			completed_at: '2025-02-08T14:30:00Z'
		},
		{
			id: 'task.add-user-authentication.2',
			plan_id: 'plan.add-user-authentication',
			sequence: 2,
			description: 'Implement UserRepository with CRUD operations',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'A valid user struct',
					when: 'Create is called',
					then: 'The user is persisted and returned with ID'
				},
				{
					given: 'An existing user email',
					when: 'FindByEmail is called',
					then: 'The user is returned'
				},
				{
					given: 'A non-existent email',
					when: 'FindByEmail is called',
					then: 'ErrNotFound is returned'
				}
			],
			files: ['internal/user/repository.go', 'internal/user/repository_test.go'],
			status: 'completed',
			created_at: '2025-02-08T14:30:00Z',
			completed_at: '2025-02-09T10:00:00Z'
		},
		{
			id: 'task.add-user-authentication.3',
			plan_id: 'plan.add-user-authentication',
			sequence: 3,
			description: 'Implement JWT token service',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'A valid user ID',
					when: 'GenerateToken is called',
					then: 'A signed JWT is returned with 24h expiry'
				},
				{
					given: 'A valid JWT',
					when: 'ValidateToken is called',
					then: 'The user ID is extracted and returned'
				},
				{
					given: 'An expired JWT',
					when: 'ValidateToken is called',
					then: 'ErrTokenExpired is returned'
				}
			],
			files: ['internal/auth/jwt.go', 'internal/auth/jwt_test.go'],
			status: 'completed',
			created_at: '2025-02-09T10:00:00Z',
			completed_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'task.add-user-authentication.4',
			plan_id: 'plan.add-user-authentication',
			sequence: 4,
			description: 'Implement login endpoint',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'Valid email and password',
					when: 'POST /auth/login is called',
					then: 'A JWT token is returned with 200 status'
				},
				{
					given: 'Invalid email',
					when: 'POST /auth/login is called',
					then: '401 Unauthorized is returned'
				},
				{
					given: 'Wrong password',
					when: 'POST /auth/login is called',
					then: '401 Unauthorized is returned'
				}
			],
			files: ['cmd/api/handlers/auth.go', 'cmd/api/handlers/auth_test.go'],
			status: 'in_progress',
			created_at: '2025-02-09T16:00:00Z',
			assigned_loop_id: 'loop_abc123',
			iteration: 2,
			max_iterations: 3,
			rejection: {
				type: 'fixable',
				reason: 'Missing rate limiting on login attempts. Add rate limiter middleware.',
				iteration: 1,
				rejected_at: '2025-02-10T10:00:00Z'
			}
		},
		{
			id: 'task.add-user-authentication.5',
			plan_id: 'plan.add-user-authentication',
			sequence: 5,
			description: 'Implement registration endpoint',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'Valid email and password',
					when: 'POST /auth/register is called',
					then: 'User is created and user_id returned with 201 status'
				},
				{
					given: 'Existing email',
					when: 'POST /auth/register is called',
					then: '409 Conflict is returned'
				},
				{
					given: 'Password less than 8 characters',
					when: 'POST /auth/register is called',
					then: '400 Bad Request is returned with validation error'
				}
			],
			files: ['cmd/api/handlers/auth.go'],
			status: 'pending',
			created_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'task.add-user-authentication.6',
			plan_id: 'plan.add-user-authentication',
			sequence: 6,
			description: 'Write integration tests for auth endpoints',
			type: 'test',
			acceptance_criteria: [
				{
					given: 'A test database',
					when: 'Integration tests run',
					then: 'All auth endpoints are tested end-to-end'
				},
				{
					given: 'The test suite',
					when: 'Coverage is measured',
					then: 'Auth package has >80% coverage'
				}
			],
			files: ['cmd/api/handlers/auth_integration_test.go'],
			status: 'pending',
			created_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'task.add-user-authentication.7',
			plan_id: 'plan.add-user-authentication',
			sequence: 7,
			description: 'Update API documentation',
			type: 'document',
			acceptance_criteria: [
				{
					given: 'The existing API docs',
					when: 'Documentation is updated',
					then: 'All auth endpoints are documented with examples'
				}
			],
			files: ['docs/api.md'],
			status: 'pending',
			created_at: '2025-02-09T16:00:00Z'
		}
	],
	'refactor-database-layer': [
		{
			id: 'task.refactor-database-layer.1',
			plan_id: 'plan.refactor-database-layer',
			sequence: 1,
			description: 'Add connection pool configuration',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'Database configuration',
					when: 'Pool settings are applied',
					then: 'MaxOpenConns, MaxIdleConns, ConnMaxLifetime are configurable'
				}
			],
			files: ['internal/database/config.go'],
			status: 'pending',
			created_at: '2025-02-10T17:00:00Z'
		},
		{
			id: 'task.refactor-database-layer.2',
			plan_id: 'plan.refactor-database-layer',
			sequence: 2,
			description: 'Implement connection pool wrapper',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'The database package',
					when: 'Pool is initialized',
					then: 'Connections are reused across requests'
				},
				{
					given: 'Pool at capacity',
					when: 'New connection requested',
					then: 'Request waits or times out gracefully'
				}
			],
			files: ['internal/database/pool.go', 'internal/database/pool_test.go'],
			status: 'pending',
			created_at: '2025-02-10T17:00:00Z'
		},
		{
			id: 'task.refactor-database-layer.3',
			plan_id: 'plan.refactor-database-layer',
			sequence: 3,
			description: 'Update repositories to use pool',
			type: 'refactor',
			acceptance_criteria: [
				{
					given: 'Existing repositories',
					when: 'Refactored to use pool',
					then: 'All database operations go through the pool'
				}
			],
			files: ['internal/repository/'],
			status: 'pending',
			created_at: '2025-02-10T17:00:00Z'
		},
		{
			id: 'task.refactor-database-layer.4',
			plan_id: 'plan.refactor-database-layer',
			sequence: 4,
			description: 'Add pool metrics and monitoring',
			type: 'implement',
			acceptance_criteria: [
				{
					given: 'The pool is running',
					when: 'Metrics endpoint is called',
					then: 'Active connections, idle connections, wait time are reported'
				}
			],
			files: ['internal/database/metrics.go'],
			status: 'pending',
			created_at: '2025-02-10T17:00:00Z'
		},
		{
			id: 'task.refactor-database-layer.5',
			plan_id: 'plan.refactor-database-layer',
			sequence: 5,
			description: 'Load test and tune pool settings',
			type: 'test',
			acceptance_criteria: [
				{
					given: 'Load test scenario',
					when: '1000 concurrent requests hit the API',
					then: 'No connection exhaustion errors occur'
				},
				{
					given: 'Load test results',
					when: 'Analyzed',
					then: 'Optimal pool settings are documented'
				}
			],
			files: ['test/load/'],
			status: 'pending',
			created_at: '2025-02-10T17:00:00Z'
		}
	]
};

/**
 * Attention items derived from plans.
 */
export interface AttentionItem {
	type: 'approval_needed' | 'question_pending' | 'task_failed' | 'task_blocked' | 'rejection';
	plan_slug?: string;
	loop_id?: string;
	title: string;
	description: string;
	action_url: string;
	created_at: string;
}

export const mockAttentionItems: AttentionItem[] = [
	{
		type: 'approval_needed',
		plan_slug: 'refactor-database-layer',
		title: 'Ready to execute tasks',
		description: 'Tasks generated. Approve to begin execution.',
		action_url: '/plans/refactor-database-layer',
		created_at: '2025-02-10T17:30:00Z'
	},
	{
		type: 'rejection',
		plan_slug: 'add-user-authentication',
		title: 'Task needs attention',
		description: 'Login endpoint implementation was rejected (fixable). Developer is retrying.',
		action_url: '/plans/add-user-authentication',
		created_at: '2025-02-10T10:00:00Z'
	}
];
