import { browser } from '$app/environment';
import {
	startMockActivityStream,
	stopMockActivityStream,
	addActivityListener
} from '$lib/api/mock';
import type { ActivityEvent } from '$lib/types';

const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true';

class ActivityStore {
	recent = $state<ActivityEvent[]>([]);
	connected = $state(false);

	private eventSource: EventSource | null = null;
	private mockCleanup: (() => void) | null = null;
	private maxEvents = 100;

	connect(filter?: string): void {
		if (!browser) return;

		if (USE_MOCKS) {
			this.connectMock();
			return;
		}

		const url = filter
			? `/stream/activity?filter=${encodeURIComponent(filter)}`
			: '/stream/activity';

		this.eventSource = new EventSource(url);

		this.eventSource.onopen = () => {
			this.connected = true;
		};

		this.eventSource.onmessage = (event) => {
			const activity = JSON.parse(event.data) as ActivityEvent;
			this.addEvent(activity);
		};

		this.eventSource.onerror = () => {
			this.connected = false;
			// Reconnect after delay
			setTimeout(() => this.connect(filter), 3000);
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
