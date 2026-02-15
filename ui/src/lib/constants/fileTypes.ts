/**
 * Centralized file type constants for document source handling.
 * Used by upload modals, drag-drop zones, and file detection.
 */

/** Valid document file extensions for upload */
export const VALID_DOCUMENT_EXTENSIONS = ['.md', '.txt', '.pdf'] as const;

/** Valid MIME types for document upload */
export const VALID_DOCUMENT_MIME_TYPES = [
	'text/markdown',
	'text/plain',
	'application/pdf'
] as const;

/** Human-readable description of supported file types */
export const SUPPORTED_FILES_DESCRIPTION = 'Supports: .md, .txt, .pdf';

/** File input accept attribute value */
export const FILE_INPUT_ACCEPT = VALID_DOCUMENT_EXTENSIONS.join(',');

/**
 * Validate if a file is a supported document type.
 * Checks both extension and MIME type for robustness.
 */
export function isValidDocumentFile(file: File): boolean {
	// Check extension
	const parts = file.name.split('.');
	if (parts.length > 1) {
		const ext = '.' + parts[parts.length - 1].toLowerCase();
		if ((VALID_DOCUMENT_EXTENSIONS as readonly string[]).includes(ext)) {
			return true;
		}
	}

	// Fall back to MIME type check
	return (VALID_DOCUMENT_MIME_TYPES as readonly string[]).includes(file.type);
}

/**
 * Get the file extension from a filename.
 * Returns null if no valid extension found.
 */
export function getFileExtension(filename: string): string | null {
	const parts = filename.split('.');
	if (parts.length > 1) {
		return '.' + parts[parts.length - 1].toLowerCase();
	}
	return null;
}
