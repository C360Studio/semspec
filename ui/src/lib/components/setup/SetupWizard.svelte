<script lang="ts">
	import { setupStore } from '$lib/stores/setup.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { Check, Rule } from '$lib/api/project';

	// Step indices for the progress indicator
	const STEPS = ['Detect', 'Checklist', 'Standards'] as const;

	// Map wizard step to progress step index (0-based)
	const stepIndexMap: Record<string, number> = { detection: 0, checklist: 1, standards: 2 };
	const currentStepIndex = $derived(stepIndexMap[setupStore.step] ?? 0);

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
</script>

<div class="wizard-overlay" role="dialog" aria-modal="true" aria-labelledby="wizard-title">
	<div class="wizard">
		<!-- ── Header ────────────────────────────────────────────────────────── -->
		<div class="wizard-header">
			<div class="wizard-title">
				<Icon name="settings" size={20} />
				<h1 id="wizard-title">Project Setup</h1>
			</div>

			{#if setupStore.step === 'detection' || setupStore.step === 'checklist' || setupStore.step === 'standards'}
				<nav class="step-indicators" aria-label="Wizard steps">
					{#each STEPS as label, i}
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
						{#if i < STEPS.length - 1}
							<div class="step-connector" class:done={i < currentStepIndex}></div>
						{/if}
					{/each}
				</nav>
			{/if}
		</div>

		<!-- ── Body ──────────────────────────────────────────────────────────── -->
		<div class="wizard-body">
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
					<p class="panel-intro">
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
					<p class="panel-intro">
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
					<p class="panel-intro">
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
				<div class="state-center success-state">
					<Icon name="check-circle" size={48} />
					<h2>Project initialized</h2>

					{#if setupStore.filesWritten.length > 0}
						<div class="files-written" aria-label="Files written">
							<p class="files-label">Files written:</p>
							<ul>
								{#each setupStore.filesWritten as file}
									<li>
										<Icon name="file" size={12} />
										<code>{file}</code>
									</li>
								{/each}
							</ul>
						</div>
					{/if}

					<button class="btn btn-primary" onclick={() => setupStore.checkStatus()}>
						Go to Activity
					</button>
				</div>
			{/if}
		</div>

		<!-- ── Footer / Navigation ────────────────────────────────────────────── -->
		{#if setupStore.step === 'detection' || setupStore.step === 'checklist' || setupStore.step === 'standards'}
			<div class="wizard-footer">
				<!-- Back -->
				<button
					class="btn btn-ghost"
					onclick={() => setupStore.goBack()}
					disabled={setupStore.step === 'detection'}
					aria-label={setupStore.step === 'checklist' ? 'Back to detection review' : 'Back to checklist'}
				>
					<Icon name="chevron-left" size={16} />
					Back
				</button>

				<div class="step-label" aria-live="polite">
					Step {currentStepIndex + 1} of {STEPS.length}
				</div>

				<!-- Next / Initialize -->
				{#if setupStore.step === 'detection'}
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

	/* ── Files written ── */
	.files-written {
		text-align: left;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-4);
		max-width: 400px;
		width: 100%;
	}

	.files-label {
		margin: 0 0 var(--space-2) 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		font-weight: var(--font-weight-medium);
	}

	.files-written ul {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.files-written li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.files-written code {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
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

	/* ── Spin animation (shared with BoardView) ── */
	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
</style>
