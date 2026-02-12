/**
 * Mock data for changes/workflow management.
 * Based on semspec-ui-redesign-spec.md
 */

import type { ChangeWithStatus, AttentionItem } from '$lib/types/changes';

/**
 * Sample changes matching the spec examples
 */
export const mockChanges: ChangeWithStatus[] = [
	{
		slug: 'add-user-authentication',
		title: 'Add User Authentication with JWT Tokens',
		status: 'implementing',
		author: 'coby',
		created_at: '2025-02-08T09:00:00Z',
		updated_at: '2025-02-11T10:42:00Z',
		files: {
			has_proposal: true,
			has_design: true,
			has_spec: true,
			has_tasks: true
		},
		github: {
			epic_number: 42,
			epic_url: 'https://github.com/org/repo/issues/42',
			repository: 'org/repo',
			task_issues: { '1.1': 43, '1.2': 44, '2.1': 45 }
		},
		active_loops: [
			{
				loop_id: 'loop_abc123',
				role: 'implementer',
				model: 'qwen',
				state: 'executing',
				iterations: 3,
				max_iterations: 10,
				workflow_step: 'tasks'
			}
		],
		task_stats: { total: 7, completed: 3, failed: 0, in_progress: 1 }
	},
	{
		slug: 'refactor-database-layer',
		title: 'Refactor Database Layer for Connection Pooling',
		status: 'reviewed',
		author: 'coby',
		created_at: '2025-02-10T14:00:00Z',
		updated_at: '2025-02-11T09:30:00Z',
		files: {
			has_proposal: true,
			has_design: true,
			has_spec: true,
			has_tasks: false
		},
		active_loops: [],
		task_stats: undefined
	},
	{
		slug: 'fix-login-redirect',
		title: 'Fix Login Redirect Loop on Session Expiry',
		status: 'drafted',
		author: 'coby',
		created_at: '2025-02-11T08:00:00Z',
		updated_at: '2025-02-11T10:30:00Z',
		files: {
			has_proposal: true,
			has_design: false,
			has_spec: false,
			has_tasks: false
		},
		active_loops: [
			{
				loop_id: 'loop_def456',
				role: 'design-writer',
				model: 'claude',
				state: 'executing',
				iterations: 2,
				max_iterations: 5,
				workflow_step: 'design'
			}
		],
		task_stats: undefined
	}
];

/**
 * Sample attention items derived from the mock changes
 */
export const mockAttentionItems: AttentionItem[] = [
	{
		type: 'approval_needed',
		change_slug: 'refactor-database-layer',
		title: 'Approve spec for "refactor-database-layer"',
		description: 'The spec is ready for review and approval to generate tasks.',
		action_url: '/changes/refactor-database-layer',
		created_at: '2025-02-11T09:30:00Z'
	},
	{
		type: 'question_pending',
		change_slug: 'add-user-authentication',
		loop_id: 'loop_abc123',
		title: 'Answer question from implementer',
		description: 'Should we use bcrypt or argon2 for password hashing?',
		action_url: '/activity',
		created_at: '2025-02-11T10:40:00Z'
	}
];

/**
 * Sample document content for change detail view
 */
export const mockDocuments: Record<string, Record<string, { content: string; generated_at: string; model: string }>> = {
	'add-user-authentication': {
		proposal: {
			content: `# Add User Authentication

## Problem
The application currently has no user authentication. Users can access all endpoints without any verification.

## Proposed Solution
Implement JWT-based authentication with:
- User registration and login endpoints
- Password hashing with bcrypt
- JWT token generation and validation
- Protected route middleware

## Success Criteria
- Users can register with email/password
- Users can login and receive JWT token
- Protected routes require valid JWT
- Tokens expire after 24 hours`,
			generated_at: '2025-02-08T09:15:00Z',
			model: 'claude-3-5-sonnet'
		},
		design: {
			content: `# Authentication Design

## Components
1. **AuthService** - Handles registration, login, token generation
2. **UserRepository** - Database operations for users
3. **JWTMiddleware** - Validates tokens on protected routes
4. **PasswordHasher** - bcrypt wrapper for password operations

## API Endpoints
- POST /auth/register - Create new user
- POST /auth/login - Authenticate and get token
- POST /auth/refresh - Refresh expired token
- GET /auth/me - Get current user info

## Database Schema
\`\`\`sql
CREATE TABLE users (
  id UUID PRIMARY KEY,
  email VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
\`\`\``,
			generated_at: '2025-02-08T10:30:00Z',
			model: 'claude-3-5-sonnet'
		},
		spec: {
			content: `# Authentication Specification

## User Registration
**Endpoint**: POST /auth/register
**Request**: { email: string, password: string }
**Response**: { user_id: string, email: string }
**Validation**:
- Email must be valid format
- Password minimum 8 characters
- Email must not already exist

## User Login
**Endpoint**: POST /auth/login
**Request**: { email: string, password: string }
**Response**: { token: string, expires_at: string }
**Errors**:
- 401 if email not found
- 401 if password incorrect`,
			generated_at: '2025-02-09T14:00:00Z',
			model: 'claude-3-5-sonnet'
		},
		tasks: {
			content: `# Implementation Tasks

## Phase 1: Database
- [x] 1.1 Create user model and migration
- [x] 1.2 Add UserRepository with CRUD operations

## Phase 2: Core Auth
- [x] 2.1 Implement JWT token service
- [ ] 2.2 Implement login endpoint
- [ ] 2.3 Implement logout endpoint

## Phase 3: Testing
- [ ] 3.1 Write integration tests for auth endpoints
- [ ] 3.2 Update API documentation`,
			generated_at: '2025-02-10T08:00:00Z',
			model: 'claude-3-5-sonnet'
		}
	}
};

/**
 * Sample parsed tasks for a change
 */
export const mockParsedTasks = {
	'add-user-authentication': [
		{ id: '1.1', description: 'Create user model and migration', status: 'complete' as const },
		{ id: '1.2', description: 'Add UserRepository with CRUD operations', status: 'complete' as const },
		{ id: '2.1', description: 'Implement JWT token service', status: 'complete' as const },
		{
			id: '2.2',
			description: 'Implement login endpoint',
			status: 'in_progress' as const,
			assigned_loop_id: 'loop_abc123'
		},
		{
			id: '2.3',
			description: 'Implement logout endpoint',
			status: 'blocked' as const,
			blocked_by: ['2.2']
		},
		{
			id: '3.1',
			description: 'Write integration tests for auth endpoints',
			status: 'pending' as const
		},
		{ id: '3.2', description: 'Update API documentation', status: 'pending' as const }
	]
};
