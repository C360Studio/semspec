<script lang="ts">
	import { untrack } from 'svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { settingsStore, type Theme } from '$lib/stores/settings.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { panelState } from '$lib/stores/panelState.svelte';
	import { setupStore } from '$lib/stores/setup.svelte';
	import {
		updateConfig,
		getChecklist,
		updateChecklist,
		getStandards,
		updateStandards,
		testCheck,
		type Check,
		type Rule,
		type TestCheckResponse
	} from '$lib/api/project';

	// Local state for confirmations
	let confirmClearActivity = $state(false);
	let confirmClearMessages = $state(false);
	let confirmClearAll = $state(false);

	// ── Inline field editing ──────────────────────────────────────────────────

	// Which field is currently in edit mode: 'name' | 'org' | 'description' | null
	let editingField = $state<'name' | 'org' | 'description' | null>(null);
	let editName = $state('');
	let editOrg = $state('');
	let editDescription = $state('');
	let saving = $state(false);
	let saveError = $state<string | null>(null);

	// Slugify name to derive the platform segment
	function slugify(name: string): string {
		return name
			.toLowerCase()
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-|-$/g, '');
	}

	const derivedPlatform = $derived(
		editName ? slugify(editName) : (setupStore.status?.project_platform || 'local')
	);

	const previewPrefix = $derived(`${editOrg || '?'}.${derivedPlatform}`);

	function startEdit(field: 'name' | 'org' | 'description') {
		editName = setupStore.status?.project_name ?? '';
		editOrg = setupStore.status?.project_org ?? '';
		editDescription = setupStore.status?.project_description ?? '';
		saveError = null;
		editingField = field;
	}

	function cancelEdit() {
		editingField = null;
		saveError = null;
	}

	async function saveField() {
		if (editingField === 'org') {
			const org = editOrg.trim();
			if (!org) {
				saveError = 'Organization is required for entity IDs';
				return;
			}
			if (!/^[a-z][a-z0-9-]*$/.test(org)) {
				saveError = 'Org must be lowercase letters, numbers, and hyphens (start with letter)';
				return;
			}
		}

		saving = true;
		saveError = null;
		try {
			await updateConfig({
				name: editName.trim() || undefined,
				org: editOrg.trim() || undefined,
				description: editDescription.trim() || undefined
			});
			await untrack(() => setupStore.checkStatus());
			editingField = null;
		} catch (err) {
			if (err instanceof Error && err.message.includes('409')) {
				saveError = 'Cannot change org/platform after plans have been created';
			} else {
				saveError = err instanceof Error ? err.message : 'Failed to save';
			}
		} finally {
			saving = false;
		}
	}

	function handleFieldKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter') saveField();
		if (event.key === 'Escape') cancelEdit();
	}

	// ── Re-detect ─────────────────────────────────────────────────────────────

	let redetecting = $state(false);

	async function handleRedetect() {
		redetecting = true;
		try {
			await untrack(() => setupStore.runDetection());
		} finally {
			redetecting = false;
		}
	}

	// ── Quality Checks ────────────────────────────────────────────────────────

	let checks = $state<Check[]>([]);
	let checklistLoading = $state(true);
	let checklistError = $state<string | null>(null);
	let showAddCheck = $state(false);
	let newCheckName = $state('');
	let newCheckCommand = $state('');
	let newCheckCategory = $state<Check['category']>('test');
	let newCheckTimeout = $state('60s');
	let newCheckRequired = $state(true);
	let newCheckDescription = $state('');

	$effect(() => {
		getChecklist()
			.then((r) => {
				checks = r.checks ?? [];
				checklistLoading = false;
			})
			.catch((e) => {
				checklistError = e instanceof Error ? e.message : 'Failed to load checklist';
				checklistLoading = false;
			});
	});

	async function saveChecklist() {
		checklistError = null;
		try {
			await updateChecklist(checks);
		} catch (e) {
			checklistError = e instanceof Error ? e.message : 'Failed to save';
		}
	}

	function submitNewCheck() {
		if (!newCheckName.trim() || !newCheckCommand.trim()) return;
		checks = [
			...checks,
			{
				name: newCheckName.trim(),
				command: newCheckCommand.trim(),
				category: newCheckCategory,
				timeout: newCheckTimeout.trim() || '60s',
				required: newCheckRequired,
				description: newCheckDescription.trim(),
				trigger: []
			}
		];
		newCheckName = '';
		newCheckCommand = '';
		newCheckCategory = 'test';
		newCheckTimeout = '60s';
		newCheckRequired = true;
		newCheckDescription = '';
		showAddCheck = false;
		saveChecklist();
	}

	function toggleCheckRequired(index: number) {
		checks = checks.map((c, i) => (i === index ? { ...c, required: !c.required } : c));
		saveChecklist();
	}

	function removeCheck(index: number) {
		checks = checks.filter((_, i) => i !== index);
		saveChecklist();
	}

	let testingCheck = $state<number | null>(null);
	let testResult = $state<TestCheckResponse | null>(null);
	let testResultIndex = $state<number | null>(null);

	async function handleTestCheck(index: number) {
		const check = checks[index];
		testingCheck = index;
		testResult = null;
		testResultIndex = null;
		try {
			const result = await testCheck(check.command, check.timeout);
			testResult = result;
			testResultIndex = index;
		} catch (e) {
			testResult = {
				passed: false,
				exit_code: -1,
				stderr: e instanceof Error ? e.message : 'Failed',
				duration: '0s'
			};
			testResultIndex = index;
		} finally {
			testingCheck = null;
		}
	}

	// ── Standards Rules ───────────────────────────────────────────────────────

	let rules = $state<Rule[]>([]);
	let rulesLoading = $state(true);
	let rulesError = $state<string | null>(null);
	let showAddRule = $state(false);
	let newRuleText = $state('');
	let newRuleSeverity = $state<Rule['severity']>('warning');
	let newRuleCategory = $state('');
	let newRuleOrigin = $state('user');

	$effect(() => {
		getStandards()
			.then((r) => {
				rules = r.rules ?? [];
				rulesLoading = false;
			})
			.catch((e) => {
				rulesError = e instanceof Error ? e.message : 'Failed to load standards';
				rulesLoading = false;
			});
	});

	async function saveRules() {
		rulesError = null;
		try {
			await updateStandards(rules);
		} catch (e) {
			rulesError = e instanceof Error ? e.message : 'Failed to save';
		}
	}

	function submitNewRule() {
		if (!newRuleText.trim()) return;
		const id = `rule-${Date.now()}`;
		rules = [
			...rules,
			{
				id,
				text: newRuleText.trim(),
				severity: newRuleSeverity,
				category: newRuleCategory.trim() || 'general',
				origin: newRuleOrigin.trim() || 'user'
			}
		];
		newRuleText = '';
		newRuleSeverity = 'warning';
		newRuleCategory = '';
		newRuleOrigin = 'user';
		showAddRule = false;
		saveRules();
	}

	function removeRule(index: number) {
		rules = rules.filter((_, i) => i !== index);
		saveRules();
	}

	// ── Appearance & data ────────────────────────────────────────────────────

	const themeOptions: { value: Theme; label: string }[] = [
		{ value: 'dark', label: 'Dark' },
		{ value: 'light', label: 'Light' },
		{ value: 'system', label: 'System' }
	];

	const activityLimitOptions = [50, 100, 250, 500, 1000];

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
		<!-- Project Section -->
		<section class="settings-section">
			<h2 class="section-title">Project</h2>
			<div class="settings-card">
				<!-- Status row -->
				<div class="setting-row">
					<div class="setting-info">
						<span class="setting-label">Status</span>
						<p class="setting-description">
							{#if setupStore.step === 'config_required'}
								Required configuration missing:
								{setupStore.missingConfig.join(', ')}
							{:else if setupStore.isInitialized}
								Project configured
							{:else}
								Not configured — detection will run automatically on first plan
							{/if}
						</p>
					</div>
					<span
						class="status-indicator"
						class:configured={setupStore.isInitialized && setupStore.step !== 'config_required'}
						class:warning={setupStore.step === 'config_required'}
					>
						{#if setupStore.step === 'config_required'}
							Action Required
						{:else}
							{setupStore.isInitialized ? 'Configured' : 'Pending'}
						{/if}
					</span>
				</div>

				<!-- Project Name (inline editable) -->
				<div class="setting-row">
					<div class="setting-info">
						<span class="setting-label">Project Name</span>
						{#if editingField === 'name'}
							<p class="setting-description">
								Platform segment:{editName ? ` → <code>${derivedPlatform}</code>` : ' (enter a name)'}
							</p>
						{/if}
					</div>
					{#if editingField === 'name'}
						<div class="inline-edit-active">
							<input
								type="text"
								class="setting-input"
								bind:value={editName}
								placeholder="My Project"
								onkeydown={handleFieldKeydown}
								disabled={saving}
								autofocus
							/>
							<button class="btn btn-primary btn-sm" onclick={saveField} disabled={saving}>
								{saving ? 'Saving...' : 'Save'}
							</button>
							<button class="btn btn-ghost btn-sm" onclick={cancelEdit} disabled={saving}>
								Cancel
							</button>
						</div>
					{:else}
						<div class="inline-edit">
							<span class="setting-value">
								{setupStore.status?.project_name || '—'}
							</span>
							<button
								class="edit-trigger btn-icon"
								onclick={() => startEdit('name')}
								aria-label="Edit project name"
							>
								<Icon name="edit-3" size={13} />
							</button>
						</div>
					{/if}
				</div>

				<!-- Organization (inline editable) -->
				<div class="setting-row">
					<div class="setting-info">
						<span class="setting-label">Organization</span>
						{#if editingField !== 'org'}
							<p class="setting-description">Locked after first plan</p>
						{/if}
					</div>
					{#if editingField === 'org'}
						<div class="inline-edit-active">
							<input
								type="text"
								class="setting-input mono"
								bind:value={editOrg}
								placeholder="my-org"
								onkeydown={handleFieldKeydown}
								disabled={saving}
								autofocus
							/>
							<button class="btn btn-primary btn-sm" onclick={saveField} disabled={saving}>
								{saving ? 'Saving...' : 'Save'}
							</button>
							<button class="btn btn-ghost btn-sm" onclick={cancelEdit} disabled={saving}>
								Cancel
							</button>
						</div>
					{:else}
						<div class="inline-edit">
							{#if setupStore.status?.project_org}
								<span class="setting-value mono">{setupStore.status.project_org}</span>
							{:else}
								<span class="status-indicator warning">Not set</span>
							{/if}
							<button
								class="edit-trigger btn-icon"
								onclick={() => startEdit('org')}
								aria-label="Edit organization"
							>
								<Icon name="edit-3" size={13} />
							</button>
						</div>
					{/if}
				</div>

				<!-- Description (inline editable, always visible) -->
				<div class="setting-row">
					<div class="setting-info">
						<span class="setting-label">Description</span>
					</div>
					{#if editingField === 'description'}
						<div class="inline-edit-active">
							<input
								type="text"
								class="setting-input"
								bind:value={editDescription}
								placeholder="Brief project description"
								onkeydown={handleFieldKeydown}
								disabled={saving}
								autofocus
							/>
							<button class="btn btn-primary btn-sm" onclick={saveField} disabled={saving}>
								{saving ? 'Saving...' : 'Save'}
							</button>
							<button class="btn btn-ghost btn-sm" onclick={cancelEdit} disabled={saving}>
								Cancel
							</button>
						</div>
					{:else}
						<div class="inline-edit">
							<span class="setting-value description-value">
								{setupStore.status?.project_description || '—'}
							</span>
							<button
								class="edit-trigger btn-icon"
								onclick={() => startEdit('description')}
								aria-label="Edit description"
							>
								<Icon name="edit-3" size={13} />
							</button>
						</div>
					{/if}
				</div>

				<!-- Entity Prefix (read-only) -->
				{#if setupStore.status?.entity_prefix}
					<div class="setting-row">
						<div class="setting-info">
							<span class="setting-label">Entity Prefix</span>
							<p class="setting-description">Must be unique across federated graphs</p>
						</div>
						<span class="setting-value mono">{setupStore.status.entity_prefix}</span>
					</div>
				{/if}

				<!-- Save error banner -->
				{#if saveError}
					<div class="save-error" role="alert">
						<Icon name="alert-circle" size={14} />
						<span>{saveError}</span>
					</div>
				{/if}

				<!-- Detected Stack -->
				{#if setupStore.detection?.languages?.length}
					<div class="setting-row">
						<div class="setting-info">
							<span class="setting-label">Detected Stack</span>
						</div>
						<div class="stack-tags">
							{#each setupStore.detection.languages as lang}
								<span class="stack-tag">{lang.name}</span>
							{/each}
							{#each setupStore.detection.frameworks ?? [] as fw}
								<span class="stack-tag framework">{fw.name}</span>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Config Files -->
				{#if setupStore.isInitialized}
					<div class="setting-row">
						<div class="setting-info">
							<span class="setting-label">Config Files</span>
						</div>
						<div class="config-files">
							<span class="file-badge" class:present={setupStore.status?.has_project_json}>project.json</span>
							<span class="file-badge" class:present={setupStore.status?.has_checklist}>checklist.json</span>
							<span class="file-badge" class:present={setupStore.status?.has_standards}>standards.json</span>
						</div>
					</div>
				{/if}

				<!-- Re-detect -->
				<div class="setting-row">
					<div class="setting-info">
						<span class="setting-label">Detection</span>
						<p class="setting-description">Scan the workspace for languages, frameworks, and tooling</p>
					</div>
					<button class="btn btn-secondary btn-sm" onclick={handleRedetect} disabled={redetecting}>
						{#if redetecting}
							<Icon name="loader" size={14} />
							Detecting...
						{:else}
							<Icon name="search" size={14} />
							Re-detect
						{/if}
					</button>
				</div>
			</div>
		</section>

		<!-- Quality Checks Section -->
		<section class="settings-section">
			<h2 class="section-title">Quality Checks</h2>
			<div class="settings-card">
				<div class="subsection-header">
					<p class="subsection-description">
						Quality checks run as part of the development workflow. These gates are enforced before
						work is considered complete.
					</p>
					<button
						class="btn btn-ghost btn-sm"
						onclick={() => (showAddCheck = !showAddCheck)}
						aria-expanded={showAddCheck}
					>
						<Icon name="plus" size={14} />
						Add Check
					</button>
				</div>

				{#if showAddCheck}
					<div class="add-form">
						<div class="form-row">
							<div class="form-group">
								<label for="check-name">Name</label>
								<input
									id="check-name"
									type="text"
									placeholder="e.g. Go Tests"
									bind:value={newCheckName}
								/>
							</div>
							<div class="form-group">
								<label for="check-cmd">Command</label>
								<input
									id="check-cmd"
									type="text"
									placeholder="e.g. go test ./..."
									bind:value={newCheckCommand}
								/>
							</div>
						</div>
						<div class="form-row">
							<div class="form-group">
								<label for="check-cat">Category</label>
								<select id="check-cat" bind:value={newCheckCategory}>
									<option value="compile">compile</option>
									<option value="lint">lint</option>
									<option value="typecheck">typecheck</option>
									<option value="test">test</option>
									<option value="format">format</option>
								</select>
							</div>
							<div class="form-group">
								<label for="check-timeout">Timeout</label>
								<input
									id="check-timeout"
									type="text"
									placeholder="60s"
									bind:value={newCheckTimeout}
								/>
							</div>
							<div class="form-group form-group-checkbox">
								<label>
									<input type="checkbox" bind:checked={newCheckRequired} />
									Required
								</label>
							</div>
						</div>
						<div class="form-group">
							<label for="check-desc">Description</label>
							<input
								id="check-desc"
								type="text"
								placeholder="What does this check verify?"
								bind:value={newCheckDescription}
							/>
						</div>
						<div class="form-actions">
							<button class="btn btn-ghost btn-sm" onclick={() => (showAddCheck = false)}>
								Cancel
							</button>
							<button
								class="btn btn-primary btn-sm"
								onclick={submitNewCheck}
								disabled={!newCheckName.trim() || !newCheckCommand.trim()}
							>
								Add
							</button>
						</div>
					</div>
				{/if}

				{#if checklistError}
					<div class="save-error" role="alert">
						<Icon name="alert-circle" size={14} />
						<span>{checklistError}</span>
					</div>
				{/if}

				{#if checklistLoading}
					<div class="loading-row">
						<Icon name="loader" size={16} />
						<span>Loading checks...</span>
					</div>
				{:else if checks.length === 0}
					<div class="empty-list">
						<Icon name="list-checks" size={24} />
						<p>No checks defined. Add one above.</p>
					</div>
				{:else}
					<div class="check-table-wrapper" role="region" aria-label="Quality checks list">
						<table class="check-table">
							<thead>
								<tr>
									<th scope="col">Name</th>
									<th scope="col">Command</th>
									<th scope="col">Category</th>
									<th scope="col">Required</th>
									<th scope="col"><span class="visually-hidden">Actions</span></th>
								</tr>
							</thead>
							<tbody>
								{#each checks as check, i}
									<tr>
										<td class="check-name">{check.name}</td>
										<td><code class="check-cmd">{check.command}</code></td>
										<td>
											<span class="category-badge category-{check.category}">
												{check.category}
											</span>
										</td>
										<td>
											<button
												class="toggle-btn"
												class:toggle-on={check.required}
												onclick={() => toggleCheckRequired(i)}
												aria-pressed={check.required}
												aria-label="Toggle required for {check.name}"
											>
												{check.required ? 'Yes' : 'No'}
											</button>
										</td>
										<td class="check-actions">
											<button
												class="icon-btn"
												onclick={() => handleTestCheck(i)}
												disabled={testingCheck !== null}
												aria-label="Test check {check.name}"
												title="Run this check"
											>
												{#if testingCheck === i}
													<Icon name="loader" size={14} />
												{:else}
													<Icon name="play" size={14} />
												{/if}
											</button>
											<button
												class="icon-btn danger"
												onclick={() => removeCheck(i)}
												aria-label="Remove check {check.name}"
											>
												<Icon name="trash" size={14} />
											</button>
										</td>
									</tr>
									{#if testResultIndex === i && testResult}
										<tr class="test-result-row">
											<td colspan="5">
												<div
													class="test-result"
													class:test-pass={testResult.passed}
													class:test-fail={!testResult.passed}
												>
													<span class="test-verdict">
														<Icon name={testResult.passed ? 'check-circle' : 'x-circle'} size={14} />
														{testResult.passed ? 'Passed' : 'Failed'} ({testResult.duration})
													</span>
													{#if testResult.stdout}
														<pre class="test-output">{testResult.stdout.slice(0, 500)}</pre>
													{/if}
													{#if testResult.stderr}
														<pre class="test-output test-stderr">{testResult.stderr.slice(0, 500)}</pre>
													{/if}
												</div>
											</td>
										</tr>
									{/if}
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</div>
		</section>

		<!-- Standards Rules Section -->
		<section class="settings-section">
			<h2 class="section-title">Standards Rules</h2>
			<div class="settings-card">
				<div class="subsection-header">
					<p class="subsection-description">
						Coding standards injected into the agent's context. Rules shape how code is written and
						reviewed.
					</p>
					<button
						class="btn btn-ghost btn-sm"
						onclick={() => (showAddRule = !showAddRule)}
						aria-expanded={showAddRule}
					>
						<Icon name="plus" size={14} />
						Add Rule
					</button>
				</div>

				{#if showAddRule}
					<div class="add-form">
						<div class="form-group">
							<label for="rule-text">Rule</label>
							<input
								id="rule-text"
								type="text"
								placeholder="e.g. Always return errors rather than panicking"
								bind:value={newRuleText}
							/>
						</div>
						<div class="form-row">
							<div class="form-group">
								<label for="rule-severity">Severity</label>
								<select id="rule-severity" bind:value={newRuleSeverity}>
									<option value="error">error</option>
									<option value="warning">warning</option>
									<option value="info">info</option>
								</select>
							</div>
							<div class="form-group">
								<label for="rule-category">Category</label>
								<input
									id="rule-category"
									type="text"
									placeholder="e.g. error-handling"
									bind:value={newRuleCategory}
								/>
							</div>
							<div class="form-group">
								<label for="rule-origin">Origin</label>
								<input
									id="rule-origin"
									type="text"
									placeholder="e.g. CLAUDE.md"
									bind:value={newRuleOrigin}
								/>
							</div>
						</div>
						<div class="form-actions">
							<button class="btn btn-ghost btn-sm" onclick={() => (showAddRule = false)}>
								Cancel
							</button>
							<button
								class="btn btn-primary btn-sm"
								onclick={submitNewRule}
								disabled={!newRuleText.trim()}
							>
								Add
							</button>
						</div>
					</div>
				{/if}

				{#if rulesError}
					<div class="save-error" role="alert">
						<Icon name="alert-circle" size={14} />
						<span>{rulesError}</span>
					</div>
				{/if}

				{#if rulesLoading}
					<div class="loading-row">
						<Icon name="loader" size={16} />
						<span>Loading rules...</span>
					</div>
				{:else if rules.length === 0}
					<div class="empty-list">
						<Icon name="book-open" size={24} />
						<p>No rules yet. Add rules manually.</p>
					</div>
				{:else}
					<ul class="rule-list" aria-label="Standards rules">
						{#each rules as rule, i}
							<li class="rule-item">
								<div
									class="rule-severity severity-{rule.severity}"
									aria-label="Severity: {rule.severity}"
								>
									{rule.severity}
								</div>
								<div class="rule-content">
									<p class="rule-text">{rule.text}</p>
									<div class="rule-meta">
										<span class="chip chip-sm">{rule.category}</span>
										<span class="rule-origin">from {rule.origin}</span>
									</div>
								</div>
								<button
									class="icon-btn danger"
									onclick={() => removeRule(i)}
									aria-label="Remove rule: {rule.text.slice(0, 40)}"
								>
									<Icon name="trash" size={14} />
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</section>

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
		margin: 0 auto;
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

	/* Project section */
	.status-indicator {
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.status-indicator.configured {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.status-indicator.warning {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.setting-value {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.setting-value.mono {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.description-value {
		color: var(--color-text-secondary);
		font-style: italic;
	}

	.stack-tags {
		display: flex;
		gap: var(--space-1);
		flex-wrap: wrap;
	}

	.stack-tag {
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.stack-tag.framework {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	.config-files {
		display: flex;
		gap: var(--space-1);
	}

	.file-badge {
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		font-size: 10px;
		font-family: var(--font-family-mono);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.file-badge.present {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	/* Inline edit pattern */
	.inline-edit {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.edit-trigger {
		opacity: 0;
		transition: opacity var(--transition-fast);
		background: none;
		border: none;
		cursor: pointer;
		padding: var(--space-1);
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		display: flex;
		align-items: center;
	}

	.inline-edit:hover .edit-trigger,
	.edit-trigger:focus-visible {
		opacity: 1;
	}

	.edit-trigger:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}

	.inline-edit-active {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	/* Buttons */
	.btn-secondary {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
	}

	.btn-secondary:hover:not(:disabled) {
		background: var(--color-bg-elevated, var(--color-bg-tertiary));
		color: var(--color-text-primary);
	}

	.btn-secondary:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-secondary :global(svg) {
		flex-shrink: 0;
	}

	.btn-primary {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		border: 1px solid var(--color-accent);
		border-radius: var(--radius-md);
		color: white;
		font-size: var(--font-size-sm);
		cursor: pointer;
	}

	.btn-primary:hover:not(:disabled) {
		opacity: 0.9;
	}

	.btn-primary:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-ghost {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-ghost:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}

	.icon-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-1);
		background: none;
		border: none;
		border-radius: var(--radius-sm);
		cursor: pointer;
		color: var(--color-text-muted);
		transition: all var(--transition-fast);
	}

	.icon-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.icon-btn.danger:hover {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
	}

	.setting-input {
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		min-width: 200px;
	}

	.setting-input:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.setting-input:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.setting-input.mono {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.save-error {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		margin: var(--space-2) 0;
	}

	/* Subsection headers for Quality Checks / Standards sections */
	.subsection-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-4);
		padding-bottom: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		margin-bottom: var(--space-3);
	}

	.subsection-description {
		flex: 1;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		margin: 0;
		padding-top: 2px;
	}

	/* Forms (shared with wizard pattern) */
	.add-form {
		padding: var(--space-4);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}

	.form-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		flex: 1;
	}

	.form-group-checkbox {
		flex: 0 0 auto;
		justify-content: flex-end;
	}

	.form-group label {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.form-group input,
	.form-group select {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		width: 100%;
	}

	.form-group input:focus,
	.form-group select:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.form-group input[type='checkbox'] {
		width: auto;
	}

	.form-row {
		display: flex;
		gap: var(--space-3);
		align-items: flex-end;
	}

	.form-actions {
		display: flex;
		gap: var(--space-2);
		justify-content: flex-end;
	}

	/* Check table */
	.check-table-wrapper {
		overflow-x: auto;
	}

	.check-table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--font-size-sm);
	}

	.check-table th {
		text-align: left;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		font-weight: var(--font-weight-medium);
		border-bottom: 1px solid var(--color-border);
	}

	.check-table td {
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--color-border);
		vertical-align: middle;
	}

	.check-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.check-cmd {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		background: var(--color-bg-tertiary);
		padding: 2px var(--space-1);
		border-radius: var(--radius-sm);
	}

	.category-badge {
		display: inline-block;
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
	}

	.category-test { background: var(--color-info-muted); color: var(--color-info); }
	.category-lint { background: var(--color-warning-muted); color: var(--color-warning); }
	.category-compile { background: var(--color-accent-muted); color: var(--color-accent); }
	.category-typecheck { background: var(--color-success-muted); color: var(--color-success); }
	.category-format { background: var(--color-bg-elevated); color: var(--color-text-secondary); }

	.toggle-btn {
		padding: 2px 10px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn.toggle-on {
		background: var(--color-success-muted);
		border-color: var(--color-success);
		color: var(--color-success);
	}

	/* Rule list */
	.rule-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.rule-item {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.rule-severity {
		flex-shrink: 0;
		padding: 2px 8px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
	}

	.severity-error { background: var(--color-error-muted); color: var(--color-error); }
	.severity-warning { background: var(--color-warning-muted); color: var(--color-warning); }
	.severity-info { background: var(--color-info-muted); color: var(--color-info); }

	.rule-content {
		flex: 1;
		min-width: 0;
	}

	.rule-text {
		margin: 0 0 var(--space-1) 0;
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.rule-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.chip-sm {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
	}

	.rule-origin {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	/* Loading / empty states */
	.loading-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.empty-list {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-8);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.empty-list p {
		margin: 0;
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}

	/* Check actions column */
	.check-actions {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	/* Test result rows */
	.test-result-row td {
		padding: 0 !important;
	}

	.test-result {
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
	}

	.test-pass {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.1));
		color: var(--color-success);
	}

	.test-fail {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
	}

	.test-verdict {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-weight: var(--font-weight-medium);
	}

	.test-output {
		margin: var(--space-1) 0 0;
		padding: var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		overflow-x: auto;
		max-height: 150px;
		overflow-y: auto;
		font-size: var(--font-size-xs);
		white-space: pre-wrap;
		word-break: break-all;
	}

	.test-stderr {
		color: var(--color-error);
	}
</style>
