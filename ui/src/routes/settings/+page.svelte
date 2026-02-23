<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { settingsStore, type Theme } from '$lib/stores/settings.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { panelState } from '$lib/stores/panelState.svelte';

	// Local state for confirmations
	let confirmClearActivity = $state(false);
	let confirmClearMessages = $state(false);
	let confirmClearAll = $state(false);

	// Theme options
	const themeOptions: { value: Theme; label: string }[] = [
		{ value: 'dark', label: 'Dark' },
		{ value: 'light', label: 'Light' },
		{ value: 'system', label: 'System' }
	];

	// Activity limit options
	const activityLimitOptions = [50, 100, 250, 500, 1000];

	// Derived state
	const currentTheme = $derived(settingsStore.theme);
	const currentActivityLimit = $derived(settingsStore.activityLimit);
	const currentReducedMotion = $derived(settingsStore.reducedMotion);

	function handleThemeChange(event: Event) {
		const target = event.target as HTMLSelectElement;
		settingsStore.setTheme(target.value as Theme);
	}

	function handleActivityLimitChange(event: Event) {
		const target = event.target as HTMLSelectElement;
		settingsStore.setActivityLimit(parseInt(target.value, 10));
	}

	function handleReducedMotionChange(event: Event) {
		const target = event.target as HTMLInputElement;
		settingsStore.setReducedMotion(target.checked);
	}

	function clearActivity() {
		activityStore.clear();
		confirmClearActivity = false;
	}

	function clearMessages() {
		messagesStore.clear();
		confirmClearMessages = false;
	}

	function clearAllData() {
		activityStore.clear();
		messagesStore.clear();
		panelState.resetToDefaults();
		settingsStore.resetToDefaults();
		confirmClearAll = false;
	}
</script>

<svelte:head>
	<title>Settings - Semspec</title>
</svelte:head>

<div class="settings-page">
	<header class="page-header">
		<Icon name="settings" size={24} />
		<h1>Settings</h1>
	</header>

	<div class="settings-content">
		<!-- Appearance Section -->
		<section class="settings-section">
			<h2 class="section-title">Appearance</h2>
			<div class="settings-card">
				<div class="setting-row">
					<div class="setting-info">
						<label for="theme-select" class="setting-label">Theme</label>
						<p class="setting-description">Choose how Semspec looks to you</p>
					</div>
					<select
						id="theme-select"
						class="setting-select"
						value={currentTheme}
						onchange={handleThemeChange}
					>
						{#each themeOptions as option}
							<option value={option.value}>{option.label}</option>
						{/each}
					</select>
				</div>

				<div class="setting-row">
					<div class="setting-info">
						<label for="reduced-motion" class="setting-label">Reduced Motion</label>
						<p class="setting-description">Minimize animations throughout the UI</p>
					</div>
					<label class="toggle">
						<input
							type="checkbox"
							id="reduced-motion"
							checked={currentReducedMotion}
							onchange={handleReducedMotionChange}
						/>
						<span class="toggle-slider"></span>
					</label>
				</div>
			</div>
		</section>

		<!-- Data & Storage Section -->
		<section class="settings-section">
			<h2 class="section-title">Data & Storage</h2>
			<div class="settings-card">
				<div class="setting-row">
					<div class="setting-info">
						<label for="activity-limit" class="setting-label">Activity History</label>
						<p class="setting-description">Maximum events to keep in the activity feed</p>
					</div>
					<div class="setting-with-unit">
						<select
							id="activity-limit"
							class="setting-select"
							value={currentActivityLimit}
							onchange={handleActivityLimitChange}
						>
							{#each activityLimitOptions as limit}
								<option value={limit}>{limit}</option>
							{/each}
						</select>
						<span class="unit-label">events</span>
					</div>
				</div>

				<div class="setting-row actions-row">
					<div class="setting-info">
						<span class="setting-label">Clear Data</span>
						<p class="setting-description">Remove cached data from your browser</p>
					</div>
					<div class="action-buttons">
						{#if confirmClearActivity}
							<div class="confirm-group">
								<span class="confirm-text">Clear activity?</span>
								<button class="btn btn-danger btn-sm" onclick={clearActivity}>Yes</button>
								<button class="btn btn-secondary btn-sm" onclick={() => (confirmClearActivity = false)}>No</button>
							</div>
						{:else}
							<button class="btn btn-secondary btn-sm" onclick={() => (confirmClearActivity = true)}>
								Clear Activity
							</button>
						{/if}

						{#if confirmClearMessages}
							<div class="confirm-group">
								<span class="confirm-text">Clear messages?</span>
								<button class="btn btn-danger btn-sm" onclick={clearMessages}>Yes</button>
								<button class="btn btn-secondary btn-sm" onclick={() => (confirmClearMessages = false)}>No</button>
							</div>
						{:else}
							<button class="btn btn-secondary btn-sm" onclick={() => (confirmClearMessages = true)}>
								Clear Messages
							</button>
						{/if}
					</div>
				</div>

				<div class="setting-row">
					<div class="setting-info full-width">
						{#if confirmClearAll}
							<div class="confirm-inline">
								<span class="confirm-text warning">This will reset all settings and clear all cached data. Continue?</span>
								<div class="confirm-actions">
									<button class="btn btn-danger" onclick={clearAllData}>Yes, Clear Everything</button>
									<button class="btn btn-secondary" onclick={() => (confirmClearAll = false)}>Cancel</button>
								</div>
							</div>
						{:else}
							<button class="btn btn-danger" onclick={() => (confirmClearAll = true)}>
								<Icon name="trash" size={16} />
								Clear All Cached Data
							</button>
						{/if}
					</div>
				</div>
			</div>
		</section>

		<!-- About Section -->
		<section class="settings-section">
			<h2 class="section-title">About</h2>
			<div class="settings-card">
				<div class="about-row">
					<span class="about-label">Version</span>
					<span class="about-value">0.1.0</span>
				</div>
				<div class="about-row">
					<span class="about-label">API</span>
					<span class="about-value mono">{import.meta.env.VITE_API_URL || 'http://localhost:8080'}</span>
				</div>
			</div>
		</section>
	</div>
</div>

<style>
	.settings-page {
		height: 100%;
		padding: var(--space-6);
		overflow: auto;
	}

	.page-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-6);
	}

	.page-header h1 {
		font-size: var(--font-size-2xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.settings-content {
		max-width: 640px;
	}

	.settings-section {
		margin-bottom: var(--space-6);
	}

	.section-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0 0 var(--space-3) 0;
	}

	.settings-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-4);
	}

	.setting-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-3) 0;
	}

	.setting-row:not(:last-child) {
		border-bottom: 1px solid var(--color-border);
	}

	.setting-row:first-child {
		padding-top: 0;
	}

	.setting-row:last-child {
		padding-bottom: 0;
	}

	.setting-info {
		flex: 1;
	}

	.setting-info.full-width {
		flex: none;
		width: 100%;
	}

	.setting-label {
		display: block;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		margin-bottom: var(--space-1);
	}

	.setting-description {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		margin: 0;
	}

	.setting-select {
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		min-width: 120px;
	}

	.setting-select:hover {
		border-color: var(--color-border-focus);
	}

	.setting-select:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.setting-with-unit {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.unit-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	/* Toggle switch */
	.toggle {
		position: relative;
		display: inline-block;
		width: 44px;
		height: 24px;
	}

	.toggle input {
		opacity: 0;
		width: 0;
		height: 0;
	}

	.toggle-slider {
		position: absolute;
		cursor: pointer;
		top: 0;
		left: 0;
		right: 0;
		bottom: 0;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		transition: all var(--transition-fast);
	}

	.toggle-slider::before {
		position: absolute;
		content: '';
		height: 18px;
		width: 18px;
		left: 2px;
		bottom: 2px;
		background: var(--color-text-secondary);
		border-radius: var(--radius-full);
		transition: all var(--transition-fast);
	}

	.toggle input:checked + .toggle-slider {
		background: var(--color-accent);
		border-color: var(--color-accent);
	}

	.toggle input:checked + .toggle-slider::before {
		transform: translateX(20px);
		background: white;
	}

	.toggle input:focus-visible + .toggle-slider {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	/* Action buttons */
	.actions-row {
		flex-wrap: wrap;
	}

	.action-buttons {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.btn-sm {
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-xs);
	}

	.confirm-group {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.confirm-text {
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	.confirm-text.warning {
		color: var(--color-warning);
	}

	.confirm-inline {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.confirm-actions {
		display: flex;
		gap: var(--space-2);
	}

	/* About section */
	.about-row {
		display: flex;
		justify-content: space-between;
		padding: var(--space-2) 0;
	}

	.about-row:not(:last-child) {
		border-bottom: 1px solid var(--color-border);
	}

	.about-row:first-child {
		padding-top: 0;
	}

	.about-row:last-child {
		padding-bottom: 0;
	}

	.about-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.about-value {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.about-value.mono {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}
</style>
