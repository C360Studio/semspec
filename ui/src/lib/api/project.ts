import { request } from './client';

// --- Types from backend ---

export interface InitStatus {
	initialized: boolean;
	project_name?: string;
	project_description?: string;
	has_project_json: boolean;
	has_checklist: boolean;
	has_standards: boolean;
	sop_count: number;
	workspace_path: string;
}

export interface DetectedLanguage {
	name: string;
	version: string | null;
	marker: string;
	confidence: 'high' | 'medium';
	primary?: boolean;
}

export interface DetectedFramework {
	name: string;
	language: string;
	marker: string;
	confidence: 'high' | 'medium';
}

export interface DetectedTool {
	name: string;
	category: 'linter' | 'formatter' | 'task_runner' | 'ci' | 'container' | 'test_framework' | 'type_checker';
	language?: string;
	marker: string;
}

export interface DetectedDoc {
	path: string;
	type: 'project_docs' | 'contributing' | 'claude_instructions' | 'existing_sop' | 'architecture_docs';
	size_bytes: number;
}

export interface Check {
	name: string;
	command: string;
	trigger: string[];
	category: 'compile' | 'lint' | 'typecheck' | 'test' | 'format';
	required: boolean;
	timeout: string;
	description: string;
	working_dir?: string;
}

export interface DetectionResult {
	languages: DetectedLanguage[];
	frameworks: DetectedFramework[];
	tooling: DetectedTool[];
	existing_docs: DetectedDoc[];
	proposed_checklist: Check[];
}

export interface Rule {
	id: string;
	text: string;
	severity: 'error' | 'warning' | 'info';
	category: string;
	applies_to?: string[];
	origin: string;
}

export interface GenerateStandardsResponse {
	rules: Rule[];
	token_estimate: number;
}

export interface ProjectInitInput {
	name: string;
	description?: string;
	languages: string[];
	frameworks: string[];
	repository?: string;
}

export interface InitRequest {
	project: ProjectInitInput;
	checklist: Check[];
	standards: {
		version: string;
		rules: Rule[];
	};
}

export interface InitResponse {
	success: boolean;
	files_written: string[];
}

// --- Wizard/Scaffold types (for greenfield projects) ---

export interface WizardLanguage {
	name: string;
	marker: string;
	has_ast: boolean;
}

export interface WizardFramework {
	name: string;
	language: string;
}

export interface WizardOptions {
	languages: WizardLanguage[];
	frameworks: WizardFramework[];
}

export interface ScaffoldRequest {
	languages: string[];
	frameworks: string[];
}

export interface ScaffoldResponse {
	files_created: string[];
	semspec_dir: string;
}

// --- API functions ---

export async function getStatus(): Promise<InitStatus> {
	return request<InitStatus>('/project-api/status');
}

export async function detect(): Promise<DetectionResult> {
	return request<DetectionResult>('/project-api/detect', { method: 'POST' });
}

export async function generateStandards(
	detection: DetectionResult,
	existingDocsContent: Record<string, string> = {}
): Promise<GenerateStandardsResponse> {
	return request<GenerateStandardsResponse>('/project-api/generate-standards', {
		method: 'POST',
		body: { detection, existing_docs_content: existingDocsContent }
	});
}

export async function initProject(req: InitRequest): Promise<InitResponse> {
	return request<InitResponse>('/project-api/init', {
		method: 'POST',
		body: req
	});
}

export async function getWizardOptions(): Promise<WizardOptions> {
	return request<WizardOptions>('/project-api/wizard');
}

export async function scaffold(req: ScaffoldRequest): Promise<ScaffoldResponse> {
	return request<ScaffoldResponse>('/project-api/scaffold', {
		method: 'POST',
		body: req
	});
}

export async function approve(file: string): Promise<{ file: string; approved_at: string; all_approved: boolean }> {
	return request<{ file: string; approved_at: string; all_approved: boolean }>('/project-api/approve', {
		method: 'POST',
		body: { file }
	});
}
