/**
 * Panel State Store - Manages collapsible panel visibility with localStorage persistence.
 *
 * Each panel has a unique ID and tracks whether it's open or collapsed.
 * State is persisted to localStorage so panels maintain their state across page loads.
 */

const STORAGE_KEY = 'semspec-panel-state';

interface PanelConfig {
	id: string;
	defaultOpen?: boolean;
}

class PanelStateStore {
	// Map of panel IDs to their open state
	private panelStates = $state<Record<string, boolean>>({});

	// Track registered panel defaults
	private defaults: Record<string, boolean> = {};

	// Whether we've loaded from localStorage
	private initialized = false;

	constructor() {
		// Load from localStorage on initialization (browser only)
		if (typeof window !== 'undefined') {
			this.loadFromStorage();
		}
	}

	/**
	 * Load panel states from localStorage
	 */
	private loadFromStorage(): void {
		try {
			const stored = localStorage.getItem(STORAGE_KEY);
			if (stored) {
				this.panelStates = JSON.parse(stored);
			}
			this.initialized = true;
		} catch {
			// Ignore parse errors, use defaults
			this.initialized = true;
		}
	}

	/**
	 * Save panel states to localStorage
	 */
	private saveToStorage(): void {
		if (typeof window === 'undefined') return;

		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify(this.panelStates));
		} catch {
			// Ignore storage errors (quota exceeded, etc.)
		}
	}

	/**
	 * Register a panel with its default open state.
	 * Returns the current state (from storage or default).
	 */
	register(config: PanelConfig): boolean {
		const { id, defaultOpen = true } = config;
		this.defaults[id] = defaultOpen;

		// If not in stored state, use default
		if (!(id in this.panelStates)) {
			this.panelStates[id] = defaultOpen;
		}

		return this.panelStates[id];
	}

	/**
	 * Get the open state of a panel
	 */
	isOpen(id: string): boolean {
		return this.panelStates[id] ?? this.defaults[id] ?? true;
	}

	/**
	 * Toggle a panel's open state
	 */
	toggle(id: string): void {
		this.panelStates[id] = !this.isOpen(id);
		this.saveToStorage();
	}

	/**
	 * Set a panel's open state directly
	 */
	setOpen(id: string, open: boolean): void {
		this.panelStates[id] = open;
		this.saveToStorage();
	}

	/**
	 * Open all registered panels
	 */
	openAll(): void {
		for (const id of Object.keys(this.defaults)) {
			this.panelStates[id] = true;
		}
		this.saveToStorage();
	}

	/**
	 * Close all registered panels
	 */
	closeAll(): void {
		for (const id of Object.keys(this.defaults)) {
			this.panelStates[id] = false;
		}
		this.saveToStorage();
	}

	/**
	 * Reset all panels to their defaults
	 */
	resetToDefaults(): void {
		for (const [id, defaultOpen] of Object.entries(this.defaults)) {
			this.panelStates[id] = defaultOpen;
		}
		this.saveToStorage();
	}

	/**
	 * Get count of open panels
	 */
	get openCount(): number {
		return Object.values(this.panelStates).filter(Boolean).length;
	}

	/**
	 * Get count of registered panels
	 */
	get totalCount(): number {
		return Object.keys(this.defaults).length;
	}
}

export const panelState = new PanelStateStore();
