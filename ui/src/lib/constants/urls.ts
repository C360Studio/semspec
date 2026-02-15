/**
 * URL validation and detection utilities.
 * Used by chat input detection and source addition.
 */

/**
 * Validate that a string is a valid HTTP/HTTPS URL.
 */
export function isValidHttpUrl(urlString: string): boolean {
	try {
		const url = new URL(urlString);
		return url.protocol === 'http:' || url.protocol === 'https:';
	} catch {
		return false;
	}
}

/**
 * Clean a URL by removing trailing punctuation that may have been
 * accidentally included when copying from text.
 */
export function cleanUrl(url: string): string {
	// Remove trailing punctuation that's likely not part of the URL
	return url.replace(/[.!?,;:)\]}>]+$/, '');
}

/**
 * Extract the first HTTP/HTTPS URL from a string.
 * Returns null if no valid URL found.
 */
export function extractUrl(text: string): string | null {
	// Match URLs, being careful about trailing punctuation
	const pattern = /https?:\/\/[^\s<>"]+/gi;
	const match = text.match(pattern);

	if (match && match.length > 0) {
		const cleaned = cleanUrl(match[0]);
		return isValidHttpUrl(cleaned) ? cleaned : null;
	}

	return null;
}

/**
 * Extract a file path from text that ends with a supported extension.
 * Supports Unix paths (~/, ./, /) and basic relative paths.
 */
export function extractFilePath(text: string, extensions: readonly string[]): string | null {
	// Build extension pattern from provided extensions
	const extPattern = extensions.map((e) => e.replace('.', '\\.')).join('|');

	// Match paths that start with ~, ., or / and end with valid extension
	// Allow at start of string or after whitespace
	const pattern = new RegExp(`(?:^|\\s)([~./][\\w/.~-]*(?:${extPattern}))(?:\\s|$)`, 'i');
	const match = text.match(pattern);

	return match ? match[1] : null;
}
