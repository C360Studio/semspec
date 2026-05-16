import { request } from './client';

/**
 * Phase artifact descriptor. Mirrors
 * `processor/plan-manager/http_artifacts.go:PhaseArtifact`.
 */
export interface PhaseArtifact {
	/** Stable identifier without extension: "architecture", "plan", etc. */
	name: PhaseArtifactName;
	/** On-disk filename including the .md extension. */
	filename: string;
	/** File size in bytes. */
	size: number;
	/** RFC3339 mtime. */
	modified_at: string;
}

export interface PhaseArtifactsResponse {
	slug: string;
	artifacts: PhaseArtifact[];
}

/**
 * Canonical phase-artifact identifiers. Matches the backend allowList in
 * `http_artifacts.go:phaseArtifactAllowList`. Listed in rendering order.
 */
export const PHASE_ARTIFACT_NAMES = [
	'plan',
	'architecture',
	'requirements',
	'scenarios',
	'qa-summary',
	'run-summary'
] as const;

export type PhaseArtifactName = (typeof PHASE_ARTIFACT_NAMES)[number];

const PHASE_ARTIFACT_LABELS: Record<PhaseArtifactName, string> = {
	plan: 'Plan',
	architecture: 'Architecture',
	requirements: 'Requirements',
	scenarios: 'Scenarios',
	'qa-summary': 'QA Summary',
	'run-summary': 'Run Summary'
};

export function phaseArtifactLabel(name: PhaseArtifactName): string {
	return PHASE_ARTIFACT_LABELS[name];
}

export async function fetchPhaseArtifacts(slug: string): Promise<PhaseArtifactsResponse> {
	return request<PhaseArtifactsResponse>(
		`/plan-manager/plans/${encodeURIComponent(slug)}/artifacts`
	);
}

/**
 * Fetch the raw markdown body of one phase artifact. Returns null when the
 * artifact has not been written yet (404); other errors throw so the caller
 * can surface them. Markdown bypasses the standard JSON `request` helper
 * because the endpoint sends `text/markdown`.
 */
export async function fetchPhaseArtifactContent(
	slug: string,
	name: PhaseArtifactName
): Promise<string | null> {
	const response = await fetch(
		`/plan-manager/plans/${encodeURIComponent(slug)}/artifacts/${encodeURIComponent(name)}`,
		{ headers: { Accept: 'text/markdown' } }
	);
	if (response.status === 404) {
		return null;
	}
	if (!response.ok) {
		throw new Error(`Failed to load ${name}: ${response.status} ${response.statusText}`);
	}
	return response.text();
}
