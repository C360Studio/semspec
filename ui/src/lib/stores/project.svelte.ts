/**
 * Store for managing the current project context.
 * Projects group sources and plans together.
 */
class ProjectStore {
	/** Current project ID (defaults to 'default') */
	currentProjectId = $state<string>('default');

	/**
	 * Set the current project.
	 */
	setProject(projectId: string): void {
		this.currentProjectId = projectId;
	}

	/**
	 * Reset to the default project.
	 */
	reset(): void {
		this.currentProjectId = 'default';
	}
}

export const projectStore = new ProjectStore();
