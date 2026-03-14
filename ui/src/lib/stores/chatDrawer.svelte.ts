/**
 * Chat Bar Store - Global state for the persistent bottom chat bar.
 *
 * Replaces the old ChatDrawer overlay pattern with a persistent bottom panel
 * that is always visible. Supports collapsed (40px) and expanded (resizable) states.
 *
 * The bar can be scoped to different contexts:
 * - global: General chat accessible from anywhere
 * - plan: Chat scoped to a specific plan
 * - task: Chat scoped to a specific task
 * - question: Chat scoped to a specific question
 *
 * Backward-compatible: chatDrawerStore alias kept for callers that use open/close/toggle.
 */

import { browser } from '$app/environment';

export interface ChatDrawerContext {
	type: 'global' | 'plan' | 'task' | 'question';
	planSlug?: string;
	taskId?: string;
	questionId?: string;
}

export interface PageContextItem {
	type: string;
	id: string;
	label: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const HEIGHT_STORAGE_KEY = 'semspec-chat-bar-height';
const DEFAULT_HEIGHT = 280;
const MIN_HEIGHT = 150;

function clampHeight(h: number): number {
	if (!browser) return DEFAULT_HEIGHT;
	const maxHeight = window.innerHeight * 0.6;
	return Math.max(MIN_HEIGHT, Math.min(h, maxHeight));
}

function loadPersistedHeight(): number {
	if (!browser) return DEFAULT_HEIGHT;
	try {
		const raw = localStorage.getItem(HEIGHT_STORAGE_KEY);
		if (!raw) return DEFAULT_HEIGHT;
		const parsed = parseInt(raw, 10);
		return isNaN(parsed) ? DEFAULT_HEIGHT : clampHeight(parsed);
	} catch {
		return DEFAULT_HEIGHT;
	}
}

// ---------------------------------------------------------------------------
// Reactive state (module-level runes pattern)
// ---------------------------------------------------------------------------

let expanded = $state(false);
let height = $state(loadPersistedHeight());
let context = $state<ChatDrawerContext>({ type: 'global' });
let pageContextItems = $state<PageContextItem[]>([]);

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

function expand(): void {
	expanded = true;
}

function collapse(): void {
	expanded = false;
}

/**
 * Toggle expanded/collapsed. When opening, optionally sets the context.
 * Compatible with old chatDrawerStore.toggle(context?) signature.
 */
function toggle(ctx?: ChatDrawerContext): void {
	if (expanded) {
		collapse();
	} else {
		if (ctx) context = ctx;
		expand();
	}
}

/**
 * Open the bar with the given context. Backward-compatible with chatDrawerStore.open().
 */
function open(ctx: ChatDrawerContext): void {
	context = ctx;
	expanded = true;
}

/**
 * Close the bar. Backward-compatible with chatDrawerStore.close().
 */
function close(): void {
	expanded = false;
}

function setHeight(h: number): void {
	height = clampHeight(h);
	if (browser) {
		try {
			localStorage.setItem(HEIGHT_STORAGE_KEY, String(height));
		} catch {
			// localStorage unavailable — silently ignore
		}
	}
}

function setContext(ctx: ChatDrawerContext): void {
	context = ctx;
}

function setPageContext(items: PageContextItem[]): void {
	pageContextItems = items;
}

function clearPageContext(): void {
	pageContextItems = [];
}

// ---------------------------------------------------------------------------
// Derived helpers
// ---------------------------------------------------------------------------

function getContextTitle(): string {
	switch (context.type) {
		case 'plan':
			return `Chat - Plan: ${context.planSlug}`;
		case 'task':
			return `Chat - Task: ${context.taskId}`;
		case 'question':
			return `Chat - Question: ${context.questionId}`;
		default:
			return 'Chat';
	}
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------

export const chatBarStore = {
	// State getters
	get expanded() { return expanded; },
	get height() { return height; },
	get context() { return context; },
	get pageContextItems() { return pageContextItems; },
	get contextTitle() { return getContextTitle(); },

	// Backward-compatible isOpen alias (mirrors old ChatDrawerStore.isOpen)
	get isOpen() { return expanded; },

	// Actions
	expand,
	collapse,
	toggle,
	open,
	close,
	setHeight,
	setContext,
	setPageContext,
	clearPageContext
};

/**
 * Backward-compatible alias — keeps legacy call sites working during migration.
 * New code should import chatBarStore.
 * @deprecated Use chatBarStore instead.
 */
export const chatDrawerStore = chatBarStore;
