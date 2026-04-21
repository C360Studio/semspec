import { browser } from '$app/environment';
import {
	startMockActivityStream,
	stopMockActivityStream,
	addActivityListener
} from '$lib/api/mock';
import type { ActivityEvent } from '$lib/types';
import { settingsStore } from '$lib/stores/settings.svelte';

const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true';

type ActivityCallback = (event: ActivityEvent) => void;

class ActivityStore {
	recent = $state<ActivityEvent[]>([]);
	connected = $state(false);
	/**
	 * Tick timestamp (browser Date.now()) of the last SSE event per loop_id.
	 * Lets UIs surface "is this thing on?" — the time since last tick tells a
	 * watching human whether the model is quietly chewing or genuinely stuck.
	 * Populated from every loop_updated/loop_created event; evicted on
	 * loop_deleted or terminal completion events.
	 */
	loopLastSeen = $state<Map<string, number>>(new Map());

	private eventSource: EventSource | null = null;
	private mockCleanup: (() => void) | null = null;
	private currentFilter: string | undefined;
	private callbacks: Set<ActivityCallback> = new Set();

	// Cached derived — avoids untracked getter reads inside addEvent
	private maxEvents = $derived(settingsStore.activityLimit);

	connect(filter?: string): void {
		if (!browser) return;

		// Idempotent: if we already have an open EventSource (or a mock stream),
		// do not open another. Without this guard, any caller that re-invokes
		// connect() — directly or via a re-running $effect — leaves an orphan
		// EventSource and the server logs a reconnect burst.
		if (this.eventSource || this.mockCleanup) return;

		this.currentFilter = filter;

		if (USE_MOCKS) {
			this.connectMock();
			return;
		}

		// Use the agentic-dispatch activity endpoint
		const url = '/agentic-dispatch/activity';

		this.eventSource = new EventSource(url);

		// The agentic-dispatch SSE sends 'connected' and 'sync_complete' as named
		// events, and all loop mutations as 'event: activity' with the type
		// (loop_created/loop_updated/loop_deleted) inside data.type.
		this.eventSource.addEventListener('connected', () => {
			this.connected = true;
		});

		this.eventSource.addEventListener('activity', (event) => {
			const activity = JSON.parse((event as MessageEvent).data) as ActivityEvent;
			this.addEvent(activity);
		});

		this.eventSource.onerror = () => {
			this.connected = false;
			// Native EventSource auto-reconnect handles backoff; backend sends retry: 5000.
		};
	}

	private connectMock(): void {
		startMockActivityStream();
		this.mockCleanup = addActivityListener((event) => {
			this.addEvent(event);
		});
		this.connected = true;
	}

	private addEvent(event: ActivityEvent): void {
		this.recent = [...this.recent.slice(-(this.maxEvents - 1)), event];

		// Maintain loopLastSeen as a heartbeat timeline. Map must be replaced,
		// not mutated, to trigger Svelte 5 reactivity — Maps aren't deeply reactive.
		if (event.loop_id) {
			if (event.type === 'loop_deleted') {
				if (this.loopLastSeen.has(event.loop_id)) {
					const next = new Map(this.loopLastSeen);
					next.delete(event.loop_id);
					this.loopLastSeen = next;
				}
			} else {
				// loop_created / loop_updated / anything else with loop_id → tick
				this.loopLastSeen = new Map(this.loopLastSeen).set(event.loop_id, Date.now());
			}
		}

		// Notify all subscribers of the new event
		for (const callback of this.callbacks) {
			callback(event);
		}
	}

	/**
	 * Milliseconds since we last saw an activity event for this loop.
	 * Returns null when we have never seen the loop. Consumers render
	 * idle time labels and fire the "stalled" treatment past a threshold.
	 */
	idleMsForLoop(loopID: string): number | null {
		const lastSeen = this.loopLastSeen.get(loopID);
		if (lastSeen === undefined) return null;
		return Date.now() - lastSeen;
	}

	// Subscribe to new activity events
	onEvent(callback: ActivityCallback): () => void {
		this.callbacks.add(callback);
		return () => this.callbacks.delete(callback);
	}

	disconnect(): void {
		if (this.eventSource) {
			this.eventSource.close();
			this.eventSource = null;
		}
		if (this.mockCleanup) {
			this.mockCleanup();
			this.mockCleanup = null;
			stopMockActivityStream();
		}
		this.connected = false;
	}

	clear(): void {
		this.recent = [];
	}
}

export const activityStore = new ActivityStore();
