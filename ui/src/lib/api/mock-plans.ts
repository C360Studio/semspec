/**
 * Mock data for ADR-024 Plan → Requirement → Scenario → Task hierarchy.
 */

import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';
import type { Phase } from '$lib/types/phase';
import type { Requirement } from '$lib/types/requirement';
import type { Scenario } from '$lib/types/scenario';

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
		project_id: 'default',
		stage: 'ready_for_execution',
		github: {
			epic_number: 42,
			epic_url: 'https://github.com/org/repo/issues/42',
			repository: 'org/repo',
			task_issues: { '1': 43, '2': 44, '3': 45 }
		},
		active_loops: [],
		task_stats: {
			total: 7,
			pending_approval: 0,
			approved: 4,
			rejected: 0,
			in_progress: 0,
			completed: 3,
			failed: 0
		}
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
		project_id: 'default',
		stage: 'ready_for_execution',
		active_loops: [],
		task_stats: {
			total: 5,
			pending_approval: 5,
			approved: 0,
			rejected: 0,
			in_progress: 0,
			completed: 0,
			failed: 0
		}
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
		project_id: 'default',
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
		project_id: 'default',
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
			phase_id: 'phase.add-user-authentication.1',
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
			completed_at: '2025-02-08T14:30:00Z',
			scenario_ids: ['scenario.auth.1.1']
		},
		{
			id: 'task.add-user-authentication.2',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.1',
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
			completed_at: '2025-02-09T10:00:00Z',
			scenario_ids: ['scenario.auth.1.2']
		},
		{
			id: 'task.add-user-authentication.3',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.2',
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
			completed_at: '2025-02-09T16:00:00Z',
			scenario_ids: ['scenario.auth.2.1']
		},
		{
			id: 'task.add-user-authentication.4',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.3',
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
			status: 'pending',
			created_at: '2025-02-09T16:00:00Z',
			scenario_ids: ['scenario.auth.3.1']
		},
		{
			id: 'task.add-user-authentication.5',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.3',
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
			created_at: '2025-02-09T16:00:00Z',
			scenario_ids: ['scenario.auth.3.2']
		},
		{
			id: 'task.add-user-authentication.6',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.4',
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
			created_at: '2025-02-09T16:00:00Z',
			scenario_ids: ['scenario.auth.4.1']
		},
		{
			id: 'task.add-user-authentication.7',
			plan_id: 'plan.add-user-authentication',
			phase_id: 'phase.add-user-authentication.4',
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
			created_at: '2025-02-09T16:00:00Z',
			scenario_ids: ['scenario.auth.4.2']
		}
	],
	'refactor-database-layer': [
		{
			id: 'task.refactor-database-layer.1',
			plan_id: 'plan.refactor-database-layer',
			phase_id: 'phase.refactor-database-layer.1',
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
			created_at: '2025-02-10T17:00:00Z',
			scenario_ids: ['scenario.db.1.1']
		},
		{
			id: 'task.refactor-database-layer.2',
			plan_id: 'plan.refactor-database-layer',
			phase_id: 'phase.refactor-database-layer.2',
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
			created_at: '2025-02-10T17:00:00Z',
			scenario_ids: ['scenario.db.2.1']
		},
		{
			id: 'task.refactor-database-layer.3',
			plan_id: 'plan.refactor-database-layer',
			phase_id: 'phase.refactor-database-layer.2',
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
			created_at: '2025-02-10T17:00:00Z',
			scenario_ids: ['scenario.db.2.2']
		},
		{
			id: 'task.refactor-database-layer.4',
			plan_id: 'plan.refactor-database-layer',
			phase_id: 'phase.refactor-database-layer.3',
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
			created_at: '2025-02-10T17:00:00Z',
			scenario_ids: ['scenario.db.3.1']
		},
		{
			id: 'task.refactor-database-layer.5',
			plan_id: 'plan.refactor-database-layer',
			phase_id: 'phase.refactor-database-layer.3',
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
			created_at: '2025-02-10T17:00:00Z',
			scenario_ids: ['scenario.db.3.2']
		}
	]
};

/**
 * Sample phases for plans.
 */
export const mockPhases: Record<string, Phase[]> = {
	'add-user-authentication': [
		{
			id: 'phase.add-user-authentication.1',
			plan_id: 'plan.add-user-authentication',
			sequence: 1,
			name: 'Phase 1: Data Layer',
			description: 'Create user model and repository layer',
			status: 'complete',
			requires_approval: true,
			approved: true,
			approved_at: '2025-02-08T12:30:00Z',
			created_at: '2025-02-08T12:00:00Z',
			started_at: '2025-02-08T13:00:00Z',
			completed_at: '2025-02-09T10:00:00Z'
		},
		{
			id: 'phase.add-user-authentication.2',
			plan_id: 'plan.add-user-authentication',
			sequence: 2,
			name: 'Phase 2: Authentication Core',
			description: 'Implement JWT token service and core auth logic',
			depends_on: ['phase.add-user-authentication.1'],
			status: 'active',
			requires_approval: true,
			approved: true,
			approved_at: '2025-02-09T10:30:00Z',
			created_at: '2025-02-08T12:00:00Z',
			started_at: '2025-02-09T11:00:00Z'
		},
		{
			id: 'phase.add-user-authentication.3',
			plan_id: 'plan.add-user-authentication',
			sequence: 3,
			name: 'Phase 3: API Endpoints',
			description: 'Implement login and registration endpoints',
			depends_on: ['phase.add-user-authentication.2'],
			status: 'pending',
			requires_approval: true,
			created_at: '2025-02-08T12:00:00Z'
		},
		{
			id: 'phase.add-user-authentication.4',
			plan_id: 'plan.add-user-authentication',
			sequence: 4,
			name: 'Phase 4: Testing & Documentation',
			description: 'Write integration tests and update documentation',
			depends_on: ['phase.add-user-authentication.3'],
			status: 'pending',
			requires_approval: true,
			created_at: '2025-02-08T12:00:00Z'
		}
	],
	'refactor-database-layer': [
		{
			id: 'phase.refactor-database-layer.1',
			plan_id: 'plan.refactor-database-layer',
			sequence: 1,
			name: 'Phase 1: Configuration',
			description: 'Add connection pool configuration options',
			status: 'pending',
			requires_approval: true,
			created_at: '2025-02-10T16:00:00Z'
		},
		{
			id: 'phase.refactor-database-layer.2',
			plan_id: 'plan.refactor-database-layer',
			sequence: 2,
			name: 'Phase 2: Implementation',
			description: 'Implement pool wrapper and update repositories',
			depends_on: ['phase.refactor-database-layer.1'],
			status: 'pending',
			requires_approval: true,
			created_at: '2025-02-10T16:00:00Z'
		},
		{
			id: 'phase.refactor-database-layer.3',
			plan_id: 'plan.refactor-database-layer',
			sequence: 3,
			name: 'Phase 3: Monitoring & Testing',
			description: 'Add metrics and perform load testing',
			depends_on: ['phase.refactor-database-layer.2'],
			status: 'pending',
			requires_approval: true,
			created_at: '2025-02-10T16:00:00Z'
		}
	]
};

/**
 * Mock requirements per plan slug — ADR-024 Plan → Requirement hierarchy.
 */
export const mockRequirements: Record<string, Requirement[]> = {
	'add-user-authentication': [
		{
			id: 'req.auth.1',
			plan_id: 'plan.add-user-authentication',
			title: 'User Data Persistence',
			description:
				'The system must store user identity data durably with a unique identifier, email address, and hashed credential. The schema must support future profile extensions without breaking existing queries.',
			status: 'active',
			created_at: '2025-02-08T09:30:00Z',
			updated_at: '2025-02-08T09:30:00Z'
		},
		{
			id: 'req.auth.2',
			plan_id: 'plan.add-user-authentication',
			title: 'JWT Authentication',
			description:
				'The system must issue signed JWT tokens upon successful credential verification and validate those tokens on protected routes. Expired tokens must be rejected with a clear error.',
			status: 'active',
			depends_on: ['req.auth.1'],
			created_at: '2025-02-08T09:30:00Z',
			updated_at: '2025-02-08T09:30:00Z'
		},
		{
			id: 'req.auth.3',
			plan_id: 'plan.add-user-authentication',
			title: 'Auth API Endpoints',
			description:
				'The system must expose HTTP endpoints for user login and registration. Each endpoint must validate input, return appropriate HTTP status codes, and include a JWT in the success response.',
			status: 'active',
			depends_on: ['req.auth.2'],
			created_at: '2025-02-08T09:30:00Z',
			updated_at: '2025-02-08T09:30:00Z'
		},
		{
			id: 'req.auth.4',
			plan_id: 'plan.add-user-authentication',
			title: 'Quality Assurance',
			description:
				'The auth package must have integration test coverage above 80% and all public endpoints must be documented with request/response examples.',
			status: 'active',
			depends_on: ['req.auth.3'],
			created_at: '2025-02-08T09:30:00Z',
			updated_at: '2025-02-08T09:30:00Z'
		}
	],
	'refactor-database-layer': [
		{
			id: 'req.db.1',
			plan_id: 'plan.refactor-database-layer',
			title: 'Connection Pool Configuration',
			description:
				'The database package must support configurable pool settings — max open connections, max idle connections, and connection max lifetime — loaded from application configuration at startup.',
			status: 'active',
			created_at: '2025-02-10T14:30:00Z',
			updated_at: '2025-02-10T14:30:00Z'
		},
		{
			id: 'req.db.2',
			plan_id: 'plan.refactor-database-layer',
			title: 'Pool Implementation',
			description:
				'The system must reuse database connections across requests via a pool. When the pool is at capacity, new requests must wait with a timeout rather than failing immediately. All existing repositories must be migrated to use the pool.',
			status: 'active',
			depends_on: ['req.db.1'],
			created_at: '2025-02-10T14:30:00Z',
			updated_at: '2025-02-10T14:30:00Z'
		},
		{
			id: 'req.db.3',
			plan_id: 'plan.refactor-database-layer',
			title: 'Observability',
			description:
				'The pool must expose runtime metrics (active connections, idle connections, wait time) via the metrics endpoint. Pool settings must be validated under load to confirm no connection exhaustion occurs at peak concurrency.',
			status: 'active',
			depends_on: ['req.db.2'],
			created_at: '2025-02-10T14:30:00Z',
			updated_at: '2025-02-10T14:30:00Z'
		}
	]
};

/**
 * Mock scenarios per plan slug — ADR-024 Requirement → Scenario hierarchy.
 */
export const mockScenarios: Record<string, Scenario[]> = {
	'add-user-authentication': [
		{
			id: 'scenario.auth.1.1',
			requirement_id: 'req.auth.1',
			given: 'A clean database with no existing schema',
			when: 'The migration is applied',
			then: [
				'A users table exists with columns: id (UUID), email (unique), password_hash, created_at',
				'The id column auto-generates a UUID on insert',
				'The email column enforces a unique constraint'
			],
			status: 'passing',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-08T14:30:00Z'
		},
		{
			id: 'scenario.auth.1.2',
			requirement_id: 'req.auth.1',
			given: 'A valid user struct with email and hashed password',
			when: 'UserRepository CRUD operations are exercised',
			then: [
				'Create persists the user and returns it with a generated ID',
				'FindByEmail returns the user when the email exists',
				'FindByEmail returns ErrNotFound when the email does not exist'
			],
			status: 'passing',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T10:00:00Z'
		},
		{
			id: 'scenario.auth.2.1',
			requirement_id: 'req.auth.2',
			given: 'A valid user ID',
			when: 'The JWT service is called',
			then: [
				'GenerateToken returns a signed JWT with a 24-hour expiry',
				'ValidateToken extracts and returns the user ID from a valid JWT',
				'ValidateToken returns ErrTokenExpired for an expired JWT'
			],
			status: 'passing',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'scenario.auth.3.1',
			requirement_id: 'req.auth.3',
			given: 'A registered user with known credentials',
			when: 'POST /auth/login is called',
			then: [
				'Valid email and password returns 200 with a JWT token',
				'An invalid email returns 401 Unauthorized',
				'A wrong password returns 401 Unauthorized'
			],
			status: 'pending',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'scenario.auth.3.2',
			requirement_id: 'req.auth.3',
			given: 'The registration endpoint is live',
			when: 'POST /auth/register is called',
			then: [
				'Valid email and password creates a user and returns 201 with user_id',
				'A duplicate email returns 409 Conflict',
				'A password shorter than 8 characters returns 400 with a validation error'
			],
			status: 'pending',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'scenario.auth.4.1',
			requirement_id: 'req.auth.4',
			given: 'A test database and the full auth integration test suite',
			when: 'Tests run against a real database',
			then: [
				'All auth endpoints are exercised end-to-end',
				'Auth package test coverage is above 80%'
			],
			status: 'pending',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T16:00:00Z'
		},
		{
			id: 'scenario.auth.4.2',
			requirement_id: 'req.auth.4',
			given: 'The existing API documentation',
			when: 'Documentation is updated for the auth feature',
			then: [
				'Login and registration endpoints are documented with example request and response payloads',
				'Error codes and their meanings are described'
			],
			status: 'pending',
			created_at: '2025-02-08T09:45:00Z',
			updated_at: '2025-02-09T16:00:00Z'
		}
	],
	'refactor-database-layer': [
		{
			id: 'scenario.db.1.1',
			requirement_id: 'req.db.1',
			given: 'Application configuration containing database pool settings',
			when: 'The database package initialises',
			then: [
				'MaxOpenConns, MaxIdleConns, and ConnMaxLifetime are read from config',
				'Missing or zero values fall back to documented defaults'
			],
			status: 'pending',
			created_at: '2025-02-10T14:45:00Z',
			updated_at: '2025-02-10T14:45:00Z'
		},
		{
			id: 'scenario.db.2.1',
			requirement_id: 'req.db.2',
			given: 'The pool is initialised with a max of 10 open connections',
			when: 'Concurrent requests arrive',
			then: [
				'Connections are reused across requests rather than created fresh',
				'A request that arrives when the pool is at capacity waits up to the configured timeout',
				'A request that times out returns a clear error rather than panicking'
			],
			status: 'pending',
			created_at: '2025-02-10T14:45:00Z',
			updated_at: '2025-02-10T14:45:00Z'
		},
		{
			id: 'scenario.db.2.2',
			requirement_id: 'req.db.2',
			given: 'Existing repository implementations that open direct connections',
			when: 'Repositories are migrated to use the pool',
			then: [
				'All database operations are routed through the pool',
				'No repository opens a raw connection directly'
			],
			status: 'pending',
			created_at: '2025-02-10T14:45:00Z',
			updated_at: '2025-02-10T14:45:00Z'
		},
		{
			id: 'scenario.db.3.1',
			requirement_id: 'req.db.3',
			given: 'The pool is running and handling requests',
			when: 'The metrics endpoint is called',
			then: [
				'Active connection count is reported',
				'Idle connection count is reported',
				'Cumulative wait time is reported'
			],
			status: 'pending',
			created_at: '2025-02-10T14:45:00Z',
			updated_at: '2025-02-10T14:45:00Z'
		},
		{
			id: 'scenario.db.3.2',
			requirement_id: 'req.db.3',
			given: 'A load test scenario simulating 1000 concurrent requests',
			when: 'The load test runs against the pooled implementation',
			then: [
				'No connection exhaustion errors occur during the test',
				'Optimal pool settings are identified and documented in the test output'
			],
			status: 'pending',
			created_at: '2025-02-10T14:45:00Z',
			updated_at: '2025-02-10T14:45:00Z'
		}
	]
};

/**
 * Attention items derived from plans.
 */
export interface AttentionItem {
	type: 'approval_needed' | 'task_failed' | 'task_blocked' | 'rejection';
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
		type: 'approval_needed',
		plan_slug: 'add-user-authentication',
		title: 'Ready to execute',
		description: 'Requirements and scenarios generated. 4 tasks ready for execution.',
		action_url: '/plans/add-user-authentication',
		created_at: '2025-02-10T10:00:00Z'
	}
];
