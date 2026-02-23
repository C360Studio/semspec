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

	private eventSource: EventSource | null = null;
	private mockCleanup: (() => void) | null = null;
	private reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
	private currentFilter: string | undefined;
	private callbacks: Set<ActivityCallback> = new Set();

	// Use settings store for max events limit
	private get maxEvents(): number {
		return settingsStore.activityLimit;
	}

	connect(filter?: string): void {
		if (!browser) return;

		this.currentFilter = filter;

		if (USE_MOCKS) {
			this.connectMock();
			return;
		}

		// Use the agentic-dispatch activity endpoint
		const url = '/agentic-dispatch/activity';

		this.eventSource = new EventSource(url);

		// Handle named events from backend
		this.eventSource.addEventListener('connected', () => {
			this.connected = true;
		});

		this.eventSource.addEventListener('sync_complete', () => {
			// Initial sync done, ready for live updates
		});

		this.eventSource.addEventListener('activity', (event) => {
			const activity = JSON.parse(event.data) as ActivityEvent;
			this.addEvent(activity);
		});

		// Fallback for generic messages (onmessage handles unnamed events)
		this.eventSource.onmessage = (event) => {
			const activity = JSON.parse(event.data) as ActivityEvent;
			this.addEvent(activity);
		};

		this.eventSource.onerror = () => {
			this.connected = false;
			this.eventSource?.close();
			this.eventSource = null;
			// Reconnect after delay
			this.reconnectTimeout = setTimeout(() => this.connect(filter), 3000);
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
		// Notify all subscribers of the new event
		for (const callback of this.callbacks) {
			callback(event);
		}
	}

	// Subscribe to new activity events
	onEvent(callback: ActivityCallback): () => void {
		this.callbacks.add(callback);
		return () => this.callbacks.delete(callback);
	}

	disconnect(): void {
		if (this.reconnectTimeout) {
			clearTimeout(this.reconnectTimeout);
			this.reconnectTimeout = null;
		}
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
