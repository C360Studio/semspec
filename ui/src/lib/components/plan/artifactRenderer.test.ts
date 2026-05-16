// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import {
	artifactSectionId,
	headingId,
	renderArtifact,
	rewriteCrossLink
} from './artifactRenderer';

describe('artifactSectionId', () => {
	it('prefixes the artifact name with "artifact-"', () => {
		expect(artifactSectionId('architecture')).toBe('artifact-architecture');
		expect(artifactSectionId('qa-summary')).toBe('artifact-qa-summary');
	});
});

describe('headingId', () => {
	it('slugifies text and prefixes with the artifact name', () => {
		expect(headingId('architecture', 'Technology choices')).toBe(
			'architecture--technology-choices'
		);
	});

	it('strips punctuation and collapses whitespace', () => {
		expect(headingId('scenarios', 'REQ-001: User login!  ')).toBe(
			'scenarios--req-001-user-login'
		);
	});

	it('falls back to "section" for headings that slug away to empty', () => {
		expect(headingId('plan', '!!!')).toBe('plan--section');
	});
});

describe('rewriteCrossLink', () => {
	it('rewrites ./X.md to the section anchor', () => {
		expect(rewriteCrossLink('./architecture.md')).toBe('#artifact-architecture');
		expect(rewriteCrossLink('./qa-summary.md')).toBe('#artifact-qa-summary');
	});

	it('accepts bare X.md without ./', () => {
		expect(rewriteCrossLink('requirements.md')).toBe('#artifact-requirements');
	});

	it('forwards an in-file fragment verbatim, prefixed by the artifact', () => {
		expect(rewriteCrossLink('./scenarios.md#requirement-overview')).toBe(
			'#scenarios--requirement-overview'
		);
	});

	it('does not re-slugify a fragment that already contains punctuation', () => {
		// Authors emit fragments that match `${artifact}--${slug}` ids; the
		// rewriter must concatenate, not re-slugify, so the round trip is
		// stable for fragments that survive the first slugifier pass
		// differently from a re-run.
		expect(rewriteCrossLink('./requirements.md#REQ-001')).toBe(
			'#requirements--REQ-001'
		);
	});

	it('returns null for unknown artifact names', () => {
		expect(rewriteCrossLink('./glossary.md')).toBeNull();
	});

	it('returns null for external links', () => {
		expect(rewriteCrossLink('https://example.com/x.md')).toBeNull();
		expect(rewriteCrossLink('mailto:nobody@example.com')).toBeNull();
		expect(rewriteCrossLink('//cdn.example.com/x.md')).toBeNull();
	});

	it('returns null for non-markdown files (plan.json etc.)', () => {
		expect(rewriteCrossLink('./plan.json')).toBeNull();
	});
});

describe('renderArtifact', () => {
	it('renders headings with deterministic ids', () => {
		const md = '# Architecture: Auth\n\n## Technology choices\n\nBody.\n';
		const out = renderArtifact('architecture', md);
		expect(out.html).toContain('id="architecture--architecture-auth"');
		expect(out.html).toContain('id="architecture--technology-choices"');
		expect(out.headings).toEqual([
			{ id: 'architecture--architecture-auth', text: 'Architecture: Auth', level: 1 },
			{ id: 'architecture--technology-choices', text: 'Technology choices', level: 2 }
		]);
	});

	it('renders GFM tables', () => {
		const md = '| a | b |\n|---|---|\n| 1 | 2 |\n';
		const out = renderArtifact('architecture', md);
		expect(out.html).toContain('<table>');
		expect(out.html).toContain('<th>a</th>');
		expect(out.html).toContain('<td>1</td>');
	});

	it('rewrites cross-artifact links inline', () => {
		const md = 'See [`scenarios.md`](./scenarios.md) for the BDD list.\n';
		const out = renderArtifact('run-summary', md);
		expect(out.html).toContain('href="#artifact-scenarios"');
		expect(out.html).not.toContain('./scenarios.md');
	});

	it('leaves unknown links alone', () => {
		const md = '[external](https://example.com)\n';
		const out = renderArtifact('plan', md);
		expect(out.html).toContain('href="https://example.com"');
	});

	it('deduplicates colliding heading ids inside the same artifact', () => {
		const md = '## Overview\n\nFirst.\n\n## Overview\n\nSecond.\n';
		const out = renderArtifact('plan', md);
		expect(out.headings.map((h) => h.id)).toEqual([
			'plan--overview',
			'plan--overview-2'
		]);
	});

	it('keeps every duplicate unique across three or more collisions', () => {
		const md = '## Overview\n\nA.\n\n## Overview\n\nB.\n\n## Overview\n\nC.\n';
		const out = renderArtifact('plan', md);
		const ids = out.headings.map((h) => h.id);
		expect(ids).toEqual(['plan--overview', 'plan--overview-2', 'plan--overview-3']);
		expect(new Set(ids).size).toBe(ids.length);
	});

	it('strips dangerous inline HTML before injecting into the document', () => {
		const md = '# Title\n\n<img src=x onerror="alert(1)">\n';
		const out = renderArtifact('plan', md);
		expect(out.html).not.toContain('onerror');
	});
});
