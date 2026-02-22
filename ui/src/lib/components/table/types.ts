/**
 * Types for DataTable component
 */

export interface Column<TData> {
	/** Unique key for the column, used for sorting */
	key: string;
	/** Display label for the column header */
	label: string;
	/** Whether the column is sortable */
	sortable?: boolean;
	/** Column width (CSS value) */
	width?: string;
	/** Text alignment: 'left' (default), 'right', 'center' */
	align?: 'left' | 'right' | 'center';
	/** Optional sort comparator for custom sorting */
	compare?: (a: TData, b: TData) => number;
	/** Optional getter for the sortable value (if different from key) */
	getValue?: (item: TData) => unknown;
	/** Hide column on mobile */
	hideOnMobile?: boolean;
}

export interface StatusOption {
	value: string;
	label: string;
}
