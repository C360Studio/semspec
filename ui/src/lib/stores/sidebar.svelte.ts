/**
 * Sidebar store - manages mobile sidebar visibility
 */

let isOpen = $state(false);

export const sidebarStore = {
	get isOpen() {
		return isOpen;
	},

	open() {
		isOpen = true;
	},

	close() {
		isOpen = false;
	},

	toggle() {
		isOpen = !isOpen;
	}
};
