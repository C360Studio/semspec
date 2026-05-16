import DOMPurify from 'dompurify';
import { marked } from 'marked';
import {
	PHASE_ARTIFACT_NAMES,
	type PhaseArtifactName
} from '$lib/api/artifacts';

/** TOC entry exposed for the section navigation strip. */
export interface ArtifactHeading {
	id: string;
	text: string;
	/** 1-6, matching h1-h6. */
	level: number;
}

export interface RenderedArtifact {
	html: string;
	headings: ArtifactHeading[];
}

const PHASE_ARTIFACT_SET = new Set<string>(PHASE_ARTIFACT_NAMES);

/**
 * Stable in-page id for the wrapper section of an artifact. Keep in sync
 * with the prefix used by `headingId` and the cross-link rewriter so
 * navigation lands on the section header, not a heading inside it.
 */
export function artifactSectionId(name: PhaseArtifactName): string {
	return `artifact-${name}`;
}

/**
 * Compute a deterministic anchor id from heading text plus the owning
 * artifact name. The artifact prefix prevents collisions when two
 * artifacts contain identical headings (e.g. both have "Overview"),
 * which would otherwise cause a TOC link to jump to the wrong section.
 */
export function headingId(artifact: PhaseArtifactName, text: string): string {
	const slug = text
		.toLowerCase()
		.replace(/[^a-z0-9\s-]/g, '')
		.replace(/\s+/g, '-')
		.replace(/-+/g, '-')
		.replace(/^-|-$/g, '');
	return `${artifact}--${slug || 'section'}`;
}

/**
 * Rewrite a relative artifact link so it navigates inside the viewer.
 * Returns null when the href is not a recognised cross-artifact link
 * (caller should leave the original href alone). Accepts both
 * `./architecture.md` and bare `architecture.md` forms, and preserves a
 * trailing `#fragment` if present.
 *
 * The fragment is treated as an already-slugged anchor id (matching the
 * `${artifact}--${slug}` shape `renderArtifact` emits), not as free
 * heading text. Cross-artifact links in the Go renderer use ids that
 * came from the same slug function, so concatenating directly avoids
 * the double-slugification edge case where uppercase or punctuation in
 * the fragment would survive differently on the second pass.
 */
export function rewriteCrossLink(href: string): string | null {
	if (!href) return null;
	// External links (http(s), mailto, protocol-relative) are out of scope.
	if (/^[a-z]+:/i.test(href) || href.startsWith('//')) return null;

	const trimmed = href.startsWith('./') ? href.slice(2) : href;
	const hashIdx = trimmed.indexOf('#');
	const base = hashIdx === -1 ? trimmed : trimmed.slice(0, hashIdx);
	const frag = hashIdx === -1 ? '' : trimmed.slice(hashIdx + 1);

	if (!base.endsWith('.md')) return null;
	const name = base.slice(0, -3) as PhaseArtifactName;
	if (!PHASE_ARTIFACT_SET.has(name)) return null;

	if (frag) {
		return `#${name}--${frag}`;
	}
	return `#${artifactSectionId(name)}`;
}

/**
 * Render one artifact's markdown body to HTML, adding heading anchors
 * and rewriting cross-artifact links. The HTML is sanitized with
 * DOMPurify before any DOM walk because plan title/goal/context fields
 * flow into the source markdown without HTML escaping — sanitization
 * neutralises a stored-XSS path through user-supplied plan fields.
 */
export function renderArtifact(
	artifact: PhaseArtifactName,
	markdown: string
): RenderedArtifact {
	const rawHtml = marked.parse(markdown, {
		gfm: true,
		breaks: false,
		async: false
	}) as string;

	const safeHtml = DOMPurify.sanitize(rawHtml);

	// DOMParser is available in the browser and in jsdom-backed tests.
	const doc = new DOMParser().parseFromString(`<div>${safeHtml}</div>`, 'text/html');
	const root = doc.body.firstElementChild as HTMLElement | null;
	if (!root) {
		return { html: safeHtml, headings: [] };
	}

	const headings: ArtifactHeading[] = [];
	// Counter is keyed on the *base* id so collisions track all variants
	// against one canonical slug — without that, the third "Overview"
	// heading would lookup an empty counter and re-emit the dedup'd id of
	// the second one, producing duplicate DOM ids.
	const seen = new Map<string, number>();
	const headingNodes = root.querySelectorAll('h1, h2, h3, h4, h5, h6');
	headingNodes.forEach((node) => {
		const text = (node.textContent ?? '').trim();
		if (!text) return;
		const baseId = headingId(artifact, text);
		const count = seen.get(baseId) ?? 0;
		const id = count === 0 ? baseId : `${baseId}-${count + 1}`;
		seen.set(baseId, count + 1);
		node.setAttribute('id', id);
		headings.push({
			id,
			text,
			level: Number(node.tagName.slice(1))
		});
	});

	root.querySelectorAll('a[href]').forEach((node) => {
		const href = node.getAttribute('href') ?? '';
		const rewritten = rewriteCrossLink(href);
		if (rewritten) {
			node.setAttribute('href', rewritten);
			// Cross-artifact navigation stays in-page; suppress target overrides.
			node.removeAttribute('target');
		}
	});

	return { html: root.innerHTML, headings };
}
