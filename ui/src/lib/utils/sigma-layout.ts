/**
 * Sigma Layout Controller
 *
 * Wraps graphology-layout-forceatlas2 web worker for async layout computation.
 * Manages the start/stop lifecycle and auto-convergence so the graph settles
 * naturally without requiring manual intervention.
 *
 * The controller runs FA2 in a web worker for 3 seconds then stops automatically.
 * This keeps the graph from jittering indefinitely while still giving nodes time
 * to find stable positions.
 *
 * Ported from semdragon.
 */

import type AbstractGraph from 'graphology';
type Graph = AbstractGraph;
import FA2Layout from 'graphology-layout-forceatlas2/worker';

/**
 * Physics settings for the Force Atlas 2 layout algorithm.
 * Tuned for a medium-density semspec entity graph (~200 nodes).
 */
const DEFAULT_SETTINGS = {
	gravity: 1,
	scalingRatio: 2,
	slowDown: 5,
	barnesHutOptimize: true, // O(n log n) approximation — essential above ~100 nodes
	barnesHutTheta: 0.5,
	adjustSizes: true // Prevent node overlap using node size attributes
};

/** How long (ms) to run the layout before auto-stopping. */
const AUTO_STOP_MS = 3_000;

/**
 * Manages the Force Atlas 2 layout lifecycle for a Sigma.js graph.
 *
 * Usage:
 *   const layout = new LayoutController();
 *   layout.start(graph);        // starts FA2 in a web worker
 *   // ... 3s later, stops automatically
 *   layout.stop();              // explicit stop (e.g., component teardown)
 */
export class LayoutController {
	private layout: FA2Layout | null = null;
	private stopTimer: ReturnType<typeof setTimeout> | null = null;
	private _isRunning = false;

	get isRunning(): boolean {
		return this._isRunning;
	}

	/**
	 * Start the layout on the given graph.
	 * Stops any previously running layout first.
	 * Does nothing in SSR (no window object).
	 */
	start(graph: Graph): void {
		// Guard: FA2 web worker requires a browser environment
		if (typeof window === 'undefined') return;

		this.stop();

		// Nothing to lay out
		if (graph.order === 0) return;

		this.layout = new FA2Layout(graph, {
			settings: DEFAULT_SETTINGS
		});
		this.layout.start();
		this._isRunning = true;

		// Auto-converge: stop after AUTO_STOP_MS to prevent endless jitter
		this.stopTimer = setTimeout(() => {
			this.stop();
		}, AUTO_STOP_MS);
	}

	/**
	 * Stop the layout worker and clean up resources.
	 * Safe to call multiple times.
	 */
	stop(): void {
		if (this.stopTimer) {
			clearTimeout(this.stopTimer);
			this.stopTimer = null;
		}
		if (this.layout) {
			this.layout.stop();
			this.layout.kill();
			this.layout = null;
		}
		this._isRunning = false;
	}
}
