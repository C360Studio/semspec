/**
 * Playwright globalSetup — runs once before any test/project.
 *
 * Ensures the semspec project has its required config gate fields satisfied
 * (org + name + checklist), so tests that load `/` aren't redirected to
 * `/settings` by the layout's hard-gate effect. settings-gate.spec.ts still
 * exercises the gate-aware UI directly; this only seeds the precondition that
 * an onboarded project would already have.
 */

const API_BASE = process.env.E2E_API_BASE ?? 'http://localhost:3000';

interface InitStatus {
	initialized?: boolean;
	project_org?: string;
}

async function fetchStatus(): Promise<InitStatus> {
	const res = await fetch(`${API_BASE}/project-manager/status`);
	if (!res.ok) throw new Error(`status ${res.status}`);
	return res.json();
}

async function autoInit(): Promise<void> {
	// Mirror the UI's setupStore.autoInit() — detect languages/frameworks,
	// then POST /init with minimal defaults. The UI normally drives this on
	// first page load; globalSetup runs before any browser navigates, so we
	// have to trigger it ourselves.
	const detectRes = await fetch(`${API_BASE}/project-manager/detect`, { method: 'POST' });
	if (!detectRes.ok) throw new Error(`detect failed: ${detectRes.status} ${await detectRes.text()}`);
	const detection = (await detectRes.json()) as {
		languages?: { name: string }[];
		frameworks?: { name: string }[];
		proposed_checklist?: unknown[];
	};
	const initRes = await fetch(`${API_BASE}/project-manager/init`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			project: {
				name: 'workspace',
				languages: (detection.languages ?? []).map((l) => l.name),
				frameworks: (detection.frameworks ?? []).map((f) => f.name)
			},
			checklist: detection.proposed_checklist ?? [],
			standards: { version: '1.0.0', items: [] }
		})
	});
	if (!initRes.ok) throw new Error(`init failed: ${initRes.status} ${await initRes.text()}`);
}

async function ensureOrg(): Promise<void> {
	let status = await fetchStatus();
	if (!status.initialized) {
		await autoInit();
		status = await fetchStatus();
	}
	if (status.project_org) return;
	const res = await fetch(`${API_BASE}/project-manager/config`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ org: 'semspec' })
	});
	if (!res.ok) {
		throw new Error(`PATCH config failed: ${res.status} ${await res.text()}`);
	}
}

async function ensureFixtureChecklist(): Promise<void> {
	// hello-world-py fixture nests its Python project under api/. semspec's
	// auto-init Python checklist runs `pip install -r requirements.txt` and
	// `python3 -m pytest .` from the workspace root, which can't find the
	// requirements file or the tests. Patch the checklist with cd-prefixed
	// commands so structural validation can actually pass once the developer
	// agent writes its test file.
	if ((process.env.E2E_FIXTURE ?? 'hello-world-py') !== 'hello-world-py') return;
	const checks = [
		{
			name: 'pip-install',
			command: 'cd api && pip install --break-system-packages -q -r requirements.txt',
			category: 'setup',
			required: true
		},
		{
			name: 'pytest',
			command: 'cd api && python3 -m pytest . -q',
			category: 'test',
			required: true
		}
	];
	const res = await fetch(`${API_BASE}/project-manager/checklist`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ checks })
	});
	if (!res.ok) {
		throw new Error(`PATCH checklist failed: ${res.status} ${await res.text()}`);
	}
}

export default async function globalSetup(): Promise<void> {
	const deadline = Date.now() + 30_000;
	let lastErr: unknown;
	while (Date.now() < deadline) {
		try {
			await ensureOrg();
			await ensureFixtureChecklist();
			return;
		} catch (err) {
			lastErr = err;
			await new Promise((r) => setTimeout(r, 500));
		}
	}
	throw new Error(`global-setup: project config not ready: ${String(lastErr)}`);
}
