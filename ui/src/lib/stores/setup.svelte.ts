import * as projectApi from '$lib/api/project';
import type {
	DetectionResult,
	Check,
	Rule,
	InitStatus,
	WizardOptions,
	WizardFramework,
	ScaffoldResponse
} from '$lib/api/project';

export type WizardStep =
	| 'loading'
	| 'scaffold'
	| 'scaffolding'
	| 'detecting'
	| 'detection'
	| 'checklist'
	| 'standards'
	| 'initializing'
	| 'complete'
	| 'error';

/**
 * SetupStore manages the multi-step project initialization wizard.
 *
 * The wizard walks through:
 *   1. Detection — scan the repo for languages, frameworks, tooling
 *   2. Checklist — review and edit the proposed quality gate checks
 *   3. Standards — generate and review coding standard rules
 *   4. Initialize — write .semspec/ files to disk
 *
 * The step field drives the UI; all API calls update step on success/failure.
 */
class SetupStore {
	// Wizard step control
	step = $state<WizardStep>('loading');
	error = $state<string | null>(null);

	// Project status from the backend
	status = $state<InitStatus | null>(null);

	// Detection results from /api/project/detect
	detection = $state<DetectionResult | null>(null);

	// User-entered project metadata
	projectName = $state('');
	projectDescription = $state('');

	// Editable copy of detection.proposed_checklist
	checklist = $state<Check[]>([]);

	// Generated or user-defined coding standards rules
	rules = $state<Rule[]>([]);

	// Paths written after successful initialization
	filesWritten = $state<string[]>([]);

	// Scaffold state (greenfield projects)
	wizardOptions = $state<WizardOptions | null>(null);
	selectedLanguages = $state<string[]>([]);
	selectedFrameworks = $state<string[]>([]);
	scaffoldResult = $state<ScaffoldResponse | null>(null);
	isGreenfield = $state(false);

	// --- Derived ---

	get isInitialized(): boolean {
		return this.status?.initialized ?? false;
	}

	get primaryLanguage(): string | null {
		const primary = this.detection?.languages?.find((l) => l.primary);
		return primary?.name ?? this.detection?.languages?.[0]?.name ?? null;
	}

	/**
	 * Get frameworks that are available for the currently selected languages.
	 */
	get availableFrameworks(): WizardFramework[] {
		return (
			this.wizardOptions?.frameworks.filter((f) =>
				this.selectedLanguages.includes(f.language)
			) ?? []
		);
	}

	// --- Methods ---

	/**
	 * Check whether the project is already initialized.
	 * If not, kick off detection automatically.
	 * For greenfield (empty) projects, show scaffold step first.
	 */
	async checkStatus(): Promise<void> {
		this.step = 'loading';
		this.error = null;
		try {
			this.status = await projectApi.getStatus();
			if (this.status.initialized) {
				this.step = 'complete';
			} else {
				// Run detection first to see what we find
				await this.runDetection();

				// If detection failed, don't continue - stay in error state
				if (this.error !== null) {
					return;
				}

				// If detection found nothing useful, this is a greenfield project
				if (this.isEmptyDetection()) {
					// Load wizard options and show scaffold step
					try {
						this.wizardOptions = await projectApi.getWizardOptions();
						this.isGreenfield = true;
						this.step = 'scaffold';
					} catch {
						// If wizard endpoint fails, proceed normally with empty detection
						console.warn('[setup] wizard options failed, proceeding with detection');
					}
				}
				// else: detection found files, already at 'detection' step
			}
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to check status';
			this.step = 'error';
		}
	}

	/**
	 * Check if detection found nothing useful (greenfield project).
	 */
	isEmptyDetection(): boolean {
		return (
			!this.detection?.languages?.length && !this.detection?.frameworks?.length
		);
	}

	/**
	 * Run project detection and advance to the detection review step.
	 */
	async runDetection(): Promise<void> {
		this.step = 'detecting';
		this.error = null;
		try {
			this.detection = await projectApi.detect();
			// Copy proposed checklist so user can edit without mutating detection
			this.checklist = [...(this.detection.proposed_checklist ?? [])];
			// Auto-suggest project name from workspace path tail segment
			if (!this.projectName && this.status?.workspace_path) {
				const parts = this.status.workspace_path.split('/');
				this.projectName = parts[parts.length - 1] || 'my-project';
			}
			this.step = 'detection';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Detection failed';
			this.step = 'error';
		}
	}

	proceedToChecklist(): void {
		this.step = 'checklist';
	}

	proceedToStandards(): void {
		this.step = 'standards';
	}

	/** Navigate back one step in the wizard. */
	goBack(): void {
		if (this.step === 'checklist') this.step = 'detection';
		else if (this.step === 'standards') this.step = 'checklist';
		else if (this.step === 'detection' && this.isGreenfield) this.step = 'scaffold';
	}

	// --- Scaffold methods (greenfield projects) ---

	/**
	 * Toggle a language selection. Removes dependent frameworks when deselecting.
	 */
	toggleLanguage(name: string): void {
		if (this.selectedLanguages.includes(name)) {
			this.selectedLanguages = this.selectedLanguages.filter((l) => l !== name);
			// Remove frameworks that depend on this language
			const deps =
				this.wizardOptions?.frameworks
					.filter((f) => f.language === name)
					.map((f) => f.name) ?? [];
			this.selectedFrameworks = this.selectedFrameworks.filter(
				(f) => !deps.includes(f)
			);
		} else {
			this.selectedLanguages = [...this.selectedLanguages, name];
		}
	}

	/**
	 * Toggle a framework selection.
	 */
	toggleFramework(name: string): void {
		if (this.selectedFrameworks.includes(name)) {
			this.selectedFrameworks = this.selectedFrameworks.filter((f) => f !== name);
		} else {
			this.selectedFrameworks = [...this.selectedFrameworks, name];
		}
	}

	/**
	 * Create scaffold files for selected languages/frameworks.
	 * Then run detection on the newly created files.
	 */
	async runScaffold(): Promise<void> {
		if (!this.selectedLanguages.length) return;

		this.step = 'scaffolding';
		this.error = null;
		try {
			this.scaffoldResult = await projectApi.scaffold({
				languages: this.selectedLanguages,
				frameworks: this.selectedFrameworks
			});
			// Now run detection on the scaffolded files
			await this.runDetection();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Scaffold failed';
			this.step = 'error';
		}
	}

	/**
	 * Refresh status without resetting wizard state (for completion step).
	 */
	async refreshStatus(): Promise<void> {
		try {
			this.status = await projectApi.getStatus();
		} catch (err) {
			console.warn('[setup] failed to refresh status:', err);
		}
	}

	/**
	 * Call the generate-standards endpoint and populate rules.
	 * Fails gracefully — empty rules are acceptable.
	 */
	async generateStandards(): Promise<void> {
		if (!this.detection) return;
		try {
			const response = await projectApi.generateStandards(this.detection);
			this.rules = response.rules;
		} catch (err) {
			// Graceful degradation: the user can still proceed with no rules
			console.warn('[setup] standards generation failed:', err);
		}
	}

	// --- Checklist mutations ---

	addCheck(check: Check): void {
		this.checklist = [...this.checklist, check];
	}

	removeCheck(index: number): void {
		this.checklist = this.checklist.filter((_, i) => i !== index);
	}

	updateCheck(index: number, check: Check): void {
		this.checklist = this.checklist.map((c, i) => (i === index ? check : c));
	}

	toggleCheckRequired(index: number): void {
		this.checklist = this.checklist.map((c, i) =>
			i === index ? { ...c, required: !c.required } : c
		);
	}

	// --- Rule mutations ---

	addRule(rule: Rule): void {
		this.rules = [...this.rules, rule];
	}

	removeRule(index: number): void {
		this.rules = this.rules.filter((_, i) => i !== index);
	}

	updateRule(index: number, rule: Rule): void {
		this.rules = this.rules.map((r, i) => (i === index ? rule : r));
	}

	/**
	 * Write the project configuration files to disk.
	 * Requires detection to have run first.
	 */
	async initialize(): Promise<void> {
		if (!this.detection) return;

		this.step = 'initializing';
		this.error = null;
		try {
			const response = await projectApi.initProject({
				project: {
					name: this.projectName,
					description: this.projectDescription || undefined,
					languages: this.detection.languages.map((l) => l.name),
					frameworks: this.detection.frameworks.map((f) => f.name)
				},
				checklist: this.checklist,
				standards: {
					version: '1.0.0',
					rules: this.rules
				}
			});

			this.filesWritten = response.files_written;
			this.step = 'complete';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Initialization failed';
			this.step = 'error';
		}
	}

	/** Reset the wizard to its initial state. */
	reset(): void {
		this.step = 'loading';
		this.error = null;
		this.status = null;
		this.detection = null;
		this.projectName = '';
		this.projectDescription = '';
		this.checklist = [];
		this.rules = [];
		this.filesWritten = [];
		this.wizardOptions = null;
		this.selectedLanguages = [];
		this.selectedFrameworks = [];
		this.scaffoldResult = null;
		this.isGreenfield = false;
	}
}

export const setupStore = new SetupStore();
