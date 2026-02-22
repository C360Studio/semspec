<script lang="ts">
	import { goto } from '$app/navigation';
	import { setupStore } from '$lib/stores/setup.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import UploadModal from '$lib/components/sources/UploadModal.svelte';
	import type { Check, Rule } from '$lib/api/project';
	import type { DocCategory } from '$lib/types/source';
	import { tick } from 'svelte';

	// Step labels and indices for progress indicator - dynamic based on greenfield
	const STEPS_GREENFIELD = ['Scaffold', 'Detect', 'Checklist', 'Standards'] as const;
	const STEPS_EXISTING = ['Detect', 'Checklist', 'Standards'] as const;

	// Focus management: track step changes and focus content
	let wizardBody: HTMLElement | null = $state(null);

	// Move focus to panel intro when step changes
	// Use afterUpdate-style effect that runs after DOM updates
	$effect(() => {
		// Track the step to make effect react to step changes
		const step = setupStore.step;
		// Only for panel steps
		const isPanelStep = ['scaffold', 'detection', 'checklist', 'standards'].includes(step);

		if (isPanelStep && wizardBody) {
			// Double RAF ensures we're after paint
			requestAnimationFrame(() => {
				requestAnimationFrame(() => {
					const panelIntro = wizardBody?.querySelector('.panel-intro') as HTMLElement | null;
					panelIntro?.focus();
				});
			});
		}
	});

	const steps = $derived(setupStore.isGreenfield ? STEPS_GREENFIELD : STEPS_EXISTING);

	const stepIndexMap = $derived(
		setupStore.isGreenfield
			? { scaffold: 0, detection: 1, checklist: 2, standards: 3 }
			: { detection: 0, checklist: 1, standards: 2 }
	);
	const currentStepIndex = $derived(
		(stepIndexMap as Record<string, number>)[setupStore.step] ?? 0
	);

	// ─── New check form state ──────────────────────────────────────────────────

	let newCheckName = $state('');
	let newCheckCommand = $state('');
	let newCheckCategory = $state<Check['category']>('test');
	let newCheckRequired = $state(true);
	let newCheckTimeout = $state('60s');
	let newCheckDescription = $state('');
	let showAddCheck = $state(false);

	function submitNewCheck(): void {
		if (!newCheckName.trim() || !newCheckCommand.trim()) return;
		setupStore.addCheck({
			name: newCheckName.trim(),
			command: newCheckCommand.trim(),
			trigger: [],
			category: newCheckCategory,
			required: newCheckRequired,
			timeout: newCheckTimeout || '60s',
			description: newCheckDescription.trim()
		});
		newCheckName = '';
		newCheckCommand = '';
		newCheckCategory = 'test';
		newCheckRequired = true;
		newCheckTimeout = '60s';
		newCheckDescription = '';
		showAddCheck = false;
	}

	// ─── New rule form state ───────────────────────────────────────────────────

	let newRuleText = $state('');
	let newRuleSeverity = $state<Rule['severity']>('warning');
	let newRuleCategory = $state('');
	let newRuleOrigin = $state('user');
	let showAddRule = $state(false);
	let ruleIdCounter = 1; // Plain counter — never rendered, no reactivity needed

	function submitNewRule(): void {
		if (!newRuleText.trim()) return;
		setupStore.addRule({
			id: `rule-${Date.now()}-${ruleIdCounter}`,
			text: newRuleText.trim(),
			severity: newRuleSeverity,
			category: newRuleCategory.trim() || 'general',
			origin: newRuleOrigin.trim() || 'user'
		});
		ruleIdCounter += 1;
		newRuleText = '';
		newRuleSeverity = 'warning';
		newRuleCategory = '';
		newRuleOrigin = 'user';
		showAddRule = false;
	}

	// ─── Completion step state ────────────────────────────────────────────────

	let showUploadModal = $state(false);
	let uploadCategory = $state<DocCategory>('reference');
	let uploadPromptLabel = $state('');
	let uploadedSuggestions = $state<Set<string>>(new Set());

	// Map languages/frameworks to suggested source types
	const SOURCE_SUGGESTIONS: Record<
		string,
		{ label: string; category: DocCategory; hint: string }[]
	> = {
		Python: [
			{ label: 'Python style guide', category: 'sop', hint: 'PEP8 customizations, naming conventions' },
			{ label: 'API documentation', category: 'api', hint: 'OpenAPI specs, endpoint docs' }
		],
		Flask: [{ label: 'Flask conventions', category: 'sop', hint: 'Route patterns, error handling' }],
		Go: [
			{ label: 'Go standards', category: 'sop', hint: 'Error handling, context patterns' },
			{ label: 'API specs', category: 'api', hint: 'OpenAPI, protobuf definitions' }
		],
		TypeScript: [
			{ label: 'TypeScript config', category: 'spec', hint: 'TSConfig standards, type patterns' }
		],
		Svelte: [
			{ label: 'Component conventions', category: 'sop', hint: 'Runes patterns, state management' }
		],
		SvelteKit: [
			{ label: 'SvelteKit patterns', category: 'sop', hint: 'Load functions, routing conventions' }
		],
		_default: [
			{ label: 'Project SOP', category: 'sop', hint: 'Development workflows, review process' },
			{ label: 'Architecture docs', category: 'reference', hint: 'System design, decision records' }
		]
	};

	// Get relevant suggestions based on detected/scaffolded stack
	const sourceSuggestions = $derived(() => {
		const detected = setupStore.detection;
		const suggestions = new Map<string, { label: string; category: DocCategory; hint: string }>();

		// Add suggestions for detected languages
		for (const lang of detected?.languages ?? []) {
			for (const sug of SOURCE_SUGGESTIONS[lang.name] ?? []) {
				suggestions.set(sug.label, sug);
			}
		}

		// Add suggestions for detected frameworks
		for (const fw of detected?.frameworks ?? []) {
			for (const sug of SOURCE_SUGGESTIONS[fw.name] ?? []) {
				suggestions.set(sug.label, sug);
			}
		}

		// Always include defaults
		for (const sug of SOURCE_SUGGESTIONS['_default']) {
			if (!suggestions.has(sug.label)) {
				suggestions.set(sug.label, sug);
			}
		}

		return Array.from(suggestions.values());
	});

	function openUploadFor(suggestion: { label: string; category: DocCategory }) {
		uploadCategory = suggestion.category;
		uploadPromptLabel = suggestion.label;
		showUploadModal = true;
	}

	async function handleSourceUpload(
		file: File,
		options: { category: DocCategory; project?: string }
	) {
		await sourcesStore.upload(file, {
			category: uploadCategory,
			projectId: options.project ?? ''
		});
		uploadedSuggestions = new Set([...uploadedSuggestions, uploadPromptLabel]);
		showUploadModal = false;
		await setupStore.refreshStatus();
	}

	function goToCreatePlan() {
		goto('/');
	}
</script>

<div class="wizard-overlay" role="dialog" aria-modal="true" aria-labelledby="wizard-title">
	<div class="wizard">
		<!-- ── Header ────────────────────────────────────────────────────────── -->
		<div class="wizard-header">
			<div class="wizard-title">
				<Icon name="settings" size={20} />
				<h1 id="wizard-title">Project Setup</h1>
			</div>

			{#if setupStore.step === 'scaffold' || setupStore.step === 'detection' || setupStore.step === 'checklist' || setupStore.step === 'standards'}
				<nav class="step-indicators" aria-label="Wizard steps">
					{#each steps as label, i}
						<div
							class="step-dot"
							class:active={i === currentStepIndex}
							class:done={i < currentStepIndex}
							aria-current={i === currentStepIndex ? 'step' : undefined}
							title={label}
						>
							{#if i < currentStepIndex}
								<Icon name="check" size={12} />
							{:else}
								<span class="step-number" aria-hidden="true">{i + 1}</span>
							{/if}
						</div>
						{#if i < steps.length - 1}
							<div class="step-connector" class:done={i < currentStepIndex}></div>
						{/if}
					{/each}
				</nav>
			{/if}
		</div>

		<!-- ── Body ──────────────────────────────────────────────────────────── -->
		<div class="wizard-body" bind:this={wizardBody}>
			<!-- Loading -->
			{#if setupStore.step === 'loading'}
				<div class="state-center" role="status" aria-live="polite">
					<Icon name="loader" size={32} class="spin" />
					<p>Checking project status...</p>
				</div>

			<!-- Detecting -->
			{:else if setupStore.step === 'detecting'}
				<div class="state-center" role="status" aria-live="polite">
					<Icon name="loader" size={32} class="spin" />
					<p>Scanning repository...</p>
					<p class="hint">Detecting languages, frameworks, and tooling</p>
				</div>

			<!-- Scaffolding (creating files) -->
			{:else if setupStore.step === 'scaffolding'}
				<div class="state-center" role="status" aria-live="polite">
					<Icon name="loader" size={32} class="spin" />
					<p>Creating project files...</p>
					<p class="hint">Setting up {setupStore.selectedLanguages.join(', ')}</p>
				</div>

			<!-- Panel 0 — Scaffold (Greenfield) -->
			{:else if setupStore.step === 'scaffold'}
				<div class="panel">
					<p class="panel-intro" tabindex="-1">
						This looks like a new project. Select the languages and frameworks you want to use.
					</p>

					<!-- Language selection -->
					<section class="section">
						<h2 class="section-title">Languages</h2>
						<p class="section-hint">Select at least one language to scaffold your project.</p>
						<div class="option-grid">
							{#each setupStore.wizardOptions?.languages ?? [] as lang}
								<button
									type="button"
									class="option-card"
									class:selected={setupStore.selectedLanguages.includes(lang.name)}
									onclick={() => setupStore.toggleLanguage(lang.name)}
								>
									<Icon name="code" size={20} />
									<span class="option-name">{lang.name}</span>
									<span class="option-marker">{lang.marker}</span>
								</button>
							{/each}
						</div>
					</section>

					<!-- Framework selection (only if languages selected) -->
					{#if setupStore.availableFrameworks.length > 0}
						<section class="section">
							<h2 class="section-title">Frameworks <span class="optional">(optional)</span></h2>
							<div class="option-grid">
								{#each setupStore.availableFrameworks as fw}
									<button
										type="button"
										class="option-card"
										class:selected={setupStore.selectedFrameworks.includes(fw.name)}
										onclick={() => setupStore.toggleFramework(fw.name)}
									>
										<Icon name="layers" size={20} />
										<span class="option-name">{fw.name}</span>
										<span class="option-marker">{fw.language}</span>
									</button>
								{/each}
							</div>
						</section>
					{/if}
				</div>

			<!-- Error -->
			{:else if setupStore.step === 'error'}
				<div class="state-center error-state" role="alert">
					<Icon name="alert-circle" size={32} />
					<h2>Something went wrong</h2>
					<p class="error-message">{setupStore.error}</p>
					<button class="btn btn-primary" onclick={() => setupStore.checkStatus()}>
						<Icon name="refresh-cw" size={14} />
						Retry
					</button>
				</div>

			<!-- Panel 1 — Detection Review -->
			{:else if setupStore.step === 'detection'}
				<div class="panel">
					<p class="panel-intro" tabindex="-1">
						We scanned your repository. Review the detected technologies and enter your project
						details below.
					</p>

					<!-- Project metadata -->
					<section class="section">
						<h2 class="section-title">Project Details</h2>
						<div class="form-group">
							<label for="project-name">Project Name <span class="required" aria-hidden="true">*</span></label>
							<input
								id="project-name"
								type="text"
								placeholder="my-project"
								bind:value={setupStore.projectName}
								required
								aria-required="true"
							/>
						</div>
						<div class="form-group">
							<label for="project-desc">Description <span class="optional">(optional)</span></label>
							<input
								id="project-desc"
								type="text"
								placeholder="What does this project do?"
								bind:value={setupStore.projectDescription}
							/>
						</div>
					</section>

					<!-- Detected languages -->
					{#if setupStore.detection?.languages?.length}
						<section class="section">
							<h2 class="section-title">Languages</h2>
							<ul class="chip-list" aria-label="Detected languages">
								{#each setupStore.detection.languages as lang}
									<li class="chip" class:chip-primary={lang.primary}>
										<Icon name="code" size={12} />
										{lang.name}
										{#if lang.version}
											<span class="chip-meta">{lang.version}</span>
										{/if}
										{#if lang.primary}
											<span class="chip-badge">primary</span>
										{/if}
									</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Detected frameworks -->
					{#if setupStore.detection?.frameworks?.length}
						<section class="section">
							<h2 class="section-title">Frameworks</h2>
							<ul class="chip-list" aria-label="Detected frameworks">
								{#each setupStore.detection.frameworks as fw}
									<li class="chip">
										<Icon name="layers" size={12} />
										{fw.name}
										<span class="chip-meta">{fw.language}</span>
									</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Detected tooling -->
					{#if setupStore.detection?.tooling?.length}
						<section class="section">
							<h2 class="section-title">Tooling</h2>
							<ul class="chip-list" aria-label="Detected tooling">
								{#each setupStore.detection.tooling as tool}
									<li class="chip">
										<Icon name="wrench" size={12} />
										{tool.name}
										<span class="chip-meta">{tool.category}</span>
									</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Existing docs -->
					{#if setupStore.detection?.existing_docs?.length}
						<section class="section">
							<h2 class="section-title">Existing Documentation</h2>
							<ul class="doc-list" aria-label="Existing docs detected">
								{#each setupStore.detection.existing_docs as doc}
									<li class="doc-item">
										<Icon name="file-text" size={12} />
										<span class="doc-path">{doc.path}</span>
										<span class="chip-meta">{doc.type.replace(/_/g, ' ')}</span>
									</li>
								{/each}
							</ul>
						</section>
					{/if}
				</div>

			<!-- Panel 2 — Checklist -->
			{:else if setupStore.step === 'checklist'}
				<div class="panel">
					<p class="panel-intro" tabindex="-1">
						These quality checks will run as part of your development workflow. Add, remove, or
						toggle the required flag for each check.
					</p>

					<section class="section">
						<div class="section-header">
							<h2 class="section-title">Quality Checks</h2>
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
							<div class="add-form" role="form" aria-label="Add new check">
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

						{#if setupStore.checklist.length === 0}
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
											<th scope="col">
												<span class="visually-hidden">Actions</span>
											</th>
										</tr>
									</thead>
									<tbody>
										{#each setupStore.checklist as check, i}
											<tr>
												<td class="check-name">{check.name}</td>
												<td>
													<code class="check-cmd">{check.command}</code>
												</td>
												<td>
													<span class="category-badge category-{check.category}">
														{check.category}
													</span>
												</td>
												<td>
													<button
														class="toggle-btn"
														class:toggle-on={check.required}
														onclick={() => setupStore.toggleCheckRequired(i)}
														aria-pressed={check.required}
														aria-label="Toggle required for {check.name}"
													>
														{check.required ? 'Yes' : 'No'}
													</button>
												</td>
												<td>
													<button
														class="icon-btn danger"
														onclick={() => setupStore.removeCheck(i)}
														aria-label="Remove check {check.name}"
													>
														<Icon name="trash" size={14} />
													</button>
												</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					</section>
				</div>

			<!-- Panel 3 — Standards -->
			{:else if setupStore.step === 'standards'}
				<div class="panel">
					<p class="panel-intro" tabindex="-1">
						Coding standards are rules injected into the agent's context. Generate them from your
						existing docs, or add rules manually.
					</p>

					<section class="section">
						<div class="section-header">
							<h2 class="section-title">Standards Rules</h2>
							<div class="btn-group">
								<button
									class="btn btn-ghost btn-sm"
									onclick={() => setupStore.generateStandards()}
								>
									<Icon name="refresh-cw" size={14} />
									Generate from Docs
								</button>
								<button
									class="btn btn-ghost btn-sm"
									onclick={() => (showAddRule = !showAddRule)}
									aria-expanded={showAddRule}
								>
									<Icon name="plus" size={14} />
									Add Rule
								</button>
							</div>
						</div>

						{#if showAddRule}
							<div class="add-form" role="form" aria-label="Add new rule">
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

						{#if setupStore.rules.length === 0}
							<div class="empty-list">
								<Icon name="book-open" size={24} />
								<p>No rules yet. Generate from docs or add manually.</p>
							</div>
						{:else}
							<ul class="rule-list" aria-label="Standards rules">
								{#each setupStore.rules as rule, i}
									<li class="rule-item">
										<div class="rule-severity severity-{rule.severity}" aria-label="Severity: {rule.severity}">
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
											onclick={() => setupStore.removeRule(i)}
											aria-label="Remove rule: {rule.text.slice(0, 40)}"
										>
											<Icon name="trash" size={14} />
										</button>
									</li>
								{/each}
							</ul>
						{/if}
					</section>
				</div>

			<!-- Initializing -->
			{:else if setupStore.step === 'initializing'}
				<div class="state-center" role="status" aria-live="polite">
					<Icon name="loader" size={32} class="spin" />
					<p>Initializing project...</p>
					<p class="hint">Writing configuration files to .semspec/</p>
				</div>

			<!-- Complete -->
			{:else if setupStore.step === 'complete'}
				<div class="completion-panel">
					<div class="completion-header">
						<Icon name="check-circle" size={48} />
						<h2>Project initialized</h2>
					</div>

					<!-- Readiness Checklist -->
					<section class="readiness-section">
						<h3>Setup Complete</h3>
						<ul class="readiness-list">
							<li class="done">
								<Icon name="check" size={14} />
								Project configured
							</li>
							<li class="done">
								<Icon name="check" size={14} />
								Quality checks defined
							</li>
							<li class:done={setupStore.status?.sop_count ?? 0 > 0}>
								<Icon name={setupStore.status?.sop_count ?? 0 > 0 ? 'check' : 'circle'} size={14} />
								Sources added ({setupStore.status?.sop_count ?? 0})
								<span class="optional-tag">optional</span>
							</li>
						</ul>
					</section>

					<!-- Context-Aware Source Suggestions -->
					<section class="sources-section">
						<h3>Add Project Knowledge</h3>
						<p class="section-hint">
							Add documentation to help the agent understand your project better.
							These will be indexed into the knowledge graph.
						</p>

						<div class="source-suggestions">
							{#each sourceSuggestions() as suggestion}
								<button
									class="suggestion-card"
									class:uploaded={uploadedSuggestions.has(suggestion.label)}
									onclick={() => openUploadFor(suggestion)}
									disabled={uploadedSuggestions.has(suggestion.label)}
								>
									<div class="suggestion-content">
										<span class="suggestion-label">
											{#if uploadedSuggestions.has(suggestion.label)}
												<Icon name="check" size={14} />
											{:else}
												<Icon name="file-plus" size={14} />
											{/if}
											{suggestion.label}
										</span>
										<span class="suggestion-hint">{suggestion.hint}</span>
									</div>
									<span class="category-badge category-{suggestion.category}">
										{suggestion.category}
									</span>
								</button>
							{/each}
						</div>

						<button class="btn btn-ghost btn-sm skip-btn" onclick={goToCreatePlan}>
							Skip for now
						</button>
					</section>

					<!-- Primary CTA -->
					<div class="cta-section">
						<button class="btn btn-primary btn-lg" onclick={goToCreatePlan}>
							<Icon name="git-pull-request" size={18} />
							Create Your First Plan
						</button>
						<p class="cta-hint">Tell the agent what you want to build</p>
					</div>
				</div>
			{/if}

		<UploadModal
			open={showUploadModal}
			uploading={sourcesStore.uploading}
			progress={sourcesStore.uploadProgress}
			onclose={() => (showUploadModal = false)}
			onupload={handleSourceUpload}
		/>
		</div>

		<!-- ── Footer / Navigation ────────────────────────────────────────────── -->
		{#if setupStore.step === 'scaffold' || setupStore.step === 'detection' || setupStore.step === 'checklist' || setupStore.step === 'standards'}
			<div class="wizard-footer">
				<!-- Back -->
				<button
					class="btn btn-ghost"
					onclick={() => setupStore.goBack()}
					disabled={setupStore.step === 'scaffold' || (setupStore.step === 'detection' && !setupStore.isGreenfield)}
					aria-label={setupStore.step === 'checklist' ? 'Back to detection review' : setupStore.step === 'detection' ? 'Back to scaffold' : 'Back to checklist'}
				>
					<Icon name="chevron-left" size={16} />
					Back
				</button>

				<div class="step-label" aria-live="polite">
					Step {currentStepIndex + 1} of {steps.length}
				</div>

				<!-- Next / Scaffold / Initialize -->
				{#if setupStore.step === 'scaffold'}
					<button
						class="btn btn-primary"
						onclick={() => setupStore.runScaffold()}
						disabled={!setupStore.selectedLanguages.length}
					>
						<Icon name="folder-plus" size={16} />
						Create Project
					</button>
				{:else if setupStore.step === 'detection'}
					<button
						class="btn btn-primary"
						onclick={() => setupStore.proceedToChecklist()}
						disabled={!setupStore.projectName.trim()}
					>
						Next
						<Icon name="chevron-right" size={16} />
					</button>
				{:else if setupStore.step === 'checklist'}
					<button class="btn btn-primary" onclick={() => setupStore.proceedToStandards()}>
						Next
						<Icon name="chevron-right" size={16} />
					</button>
				{:else if setupStore.step === 'standards'}
					<button class="btn btn-primary" onclick={() => setupStore.initialize()}>
						<Icon name="check" size={16} />
						Initialize Project
					</button>
				{/if}
			</div>
		{/if}
	</div>
</div>

<style>
	/* ── Overlay ── */
	.wizard-overlay {
		position: fixed;
		inset: 0;
		background: var(--color-bg-primary);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 1000;
		padding: var(--space-4);
	}

	.wizard {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-xl);
		width: 100%;
		max-width: 760px;
		max-height: 90vh;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	/* ── Header ── */
	.wizard-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-4) var(--space-6);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.wizard-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.wizard-title h1 {
		margin: 0;
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	/* ── Step indicators ── */
	.step-indicators {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.step-dot {
		width: 28px;
		height: 28px;
		border-radius: var(--radius-full);
		border: 2px solid var(--color-border);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		transition: all var(--transition-base);
	}

	.step-dot.active {
		border-color: var(--color-accent);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.step-dot.done {
		border-color: var(--color-success);
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.step-connector {
		width: 24px;
		height: 2px;
		background: var(--color-border);
		transition: background var(--transition-base);
	}

	.step-connector.done {
		background: var(--color-success);
	}

	.step-number {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
	}

	/* ── Body ── */
	.wizard-body {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-6);
	}

	/* ── State screens ── */
	.state-center {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		min-height: 240px;
		text-align: center;
		color: var(--color-text-secondary);
	}

	.state-center h2 {
		margin: 0;
		color: var(--color-text-primary);
		font-size: var(--font-size-xl);
	}

	.state-center p {
		margin: 0;
	}

	.hint {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.error-state {
		color: var(--color-error);
	}

	.error-message {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		max-width: 400px;
	}

	.success-state {
		color: var(--color-success);
	}

	/* ── Panel ── */
	.panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
	}

	.panel-intro {
		margin: 0;
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		line-height: var(--line-height-relaxed);
	}

	/* ── Sections ── */
	.section {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.section-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.section-title {
		margin: 0;
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.section-hint {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	/* ── Scaffold option cards ── */
	.option-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: var(--space-3);
	}

	.option-card {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-4);
		background: var(--color-bg-tertiary);
		border: 2px solid var(--color-border);
		border-radius: var(--radius-lg);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.option-card:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-elevated);
	}

	.option-card.selected {
		border-color: var(--color-accent);
		background: var(--color-accent-muted);
	}

	.option-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.option-marker {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	/* ── Forms ── */
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
		background: var(--color-bg-tertiary);
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

	.add-form {
		padding: var(--space-4);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.required {
		color: var(--color-error);
	}

	.optional {
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	/* ── Chips ── */
	.chip-list {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		list-style: none;
		padding: 0;
		margin: 0;
	}

	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.chip-primary {
		border-color: var(--color-accent);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.chip-sm {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
	}

	.chip-meta {
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.chip-badge {
		background: var(--color-accent);
		color: var(--color-bg-primary);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		padding: 1px 4px;
		border-radius: var(--radius-full);
	}

	/* ── Doc list ── */
	.doc-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.doc-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.doc-path {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	/* ── Check table ── */
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

	/* ── Rule list ── */
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

	.rule-origin {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	/* ── Empty states ── */
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

	/* ── Footer ── */
	.wizard-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-4) var(--space-6);
		border-top: 1px solid var(--color-border);
		flex-shrink: 0;
		background: var(--color-bg-secondary);
	}

	.step-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	/* ── Buttons ── */
	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border: none;
		cursor: pointer;
		transition: all var(--transition-fast);
		text-decoration: none;
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-primary {
		background: var(--color-accent);
		color: var(--color-bg-primary);
	}

	.btn-primary:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.btn-ghost {
		background: transparent;
		color: var(--color-text-secondary);
		border: 1px solid var(--color-border);
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-sm {
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-xs);
	}

	.btn-group {
		display: flex;
		gap: var(--space-2);
	}

	/* ── Icon buttons ── */
	.icon-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		border: none;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.icon-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.icon-btn.danger:hover {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	/* ── Accessibility ── */
	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border-width: 0;
	}

	/* ── Completion panel ── */
	.completion-panel {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-6);
		padding: var(--space-4);
		max-width: 500px;
		margin: 0 auto;
	}

	.completion-header {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		color: var(--color-success);
	}

	.completion-header h2 {
		margin: 0;
		color: var(--color-text-primary);
	}

	/* Readiness section */
	.readiness-section {
		text-align: left;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-4);
		width: 100%;
	}

	.readiness-section h3,
	.sources-section h3 {
		margin: 0 0 var(--space-3) 0;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.readiness-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.readiness-list li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.readiness-list li.done {
		color: var(--color-success);
	}

	.optional-tag {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		background: var(--color-bg-elevated);
		padding: 1px 6px;
		border-radius: var(--radius-full);
		margin-left: auto;
	}

	/* Sources section */
	.sources-section {
		width: 100%;
		text-align: left;
	}

	.source-suggestions {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-top: var(--space-3);
	}

	.suggestion-card {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
		text-align: left;
	}

	.suggestion-card:hover:not(:disabled) {
		border-color: var(--color-accent);
		background: var(--color-bg-elevated);
	}

	.suggestion-card.uploaded {
		border-color: var(--color-success);
		background: var(--color-success-muted);
		cursor: default;
	}

	.suggestion-card:disabled {
		opacity: 0.7;
	}

	.suggestion-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.suggestion-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.suggestion-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		padding-left: calc(14px + var(--space-2));
	}

	.skip-btn {
		margin-top: var(--space-3);
	}

	/* Category badges for sources */
	.category-sop { background: var(--color-warning-muted); color: var(--color-warning); }
	.category-spec { background: var(--color-success-muted); color: var(--color-success); }
	.category-api { background: var(--color-accent-muted); color: var(--color-accent); }
	.category-reference { background: var(--color-info-muted); color: var(--color-info); }
	.category-datasheet { background: var(--color-bg-elevated); color: var(--color-text-secondary); }

	/* CTA section */
	.cta-section {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
		width: 100%;
	}

	.btn-lg {
		padding: var(--space-3) var(--space-6);
		font-size: var(--font-size-base);
	}

	.cta-hint {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	/* ── Spin animation (shared with BoardView) ── */
	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
</style>
