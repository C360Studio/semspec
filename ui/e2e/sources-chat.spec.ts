import { test, expect, testData } from './helpers/setup';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test.describe('Add Sources While Chatting', () => {
	test.beforeEach(async ({ chatPage }) => {
		await chatPage.goto();
	});

	test.describe('URL Detection', () => {
		test('shows suggestion chip when URL is pasted', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);

			await chatPage.expectSuggestionChip('url');
			await chatPage.expectSuggestionChipValue('docs.example.com');
		});

		test('shows full URL in chip title for long URLs', async ({ chatPage }) => {
			const longUrl = 'https://docs.example.com/very/long/path/to/documentation/file.html';
			await chatPage.typeMessage(longUrl);

			await chatPage.expectSuggestionChip('url');
			// Title attribute should have full URL
			await expect(chatPage.suggestionChipValue).toHaveAttribute('title', longUrl);
		});

		test('hides chip when URL is removed from input', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);
			await chatPage.expectSuggestionChip('url');

			await chatPage.typeMessage('');
			await chatPage.expectNoSuggestionChip();
		});

		test('dismisses chip when dismiss button clicked', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);
			await chatPage.expectSuggestionChip('url');

			await chatPage.dismissSuggestionChip();
			await chatPage.expectNoSuggestionChip();
		});

		test('adds URL as source when chip button clicked', async ({ chatPage, page }) => {
			// Mock the sources API
			await page.route('**/sources/web', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						id: 'src-123',
						name: 'API Reference',
						type: 'web',
						url: testData.testUrl()
					})
				});
			});

			const url = testData.testUrl();
			await chatPage.typeMessage(url);
			await chatPage.expectSuggestionChip('url');

			await chatPage.clickAddSource();
			await chatPage.expectNoSuggestionChip();
			await chatPage.expectStatusMessage('Added source');
		});

		test('shows error status when URL add fails', async ({ chatPage, page }) => {
			// Mock failed API response
			await page.route('**/sources/web', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Server error' })
				});
			});

			const url = testData.testUrl();
			await chatPage.typeMessage(url);
			await chatPage.clickAddSource();

			await chatPage.expectStatusMessage('Failed to add source');
		});

		test('clears URL from input after adding', async ({ chatPage, page }) => {
			await page.route('**/sources/web', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						id: 'src-123',
						name: 'API Reference',
						type: 'web',
						url: testData.testUrl()
					})
				});
			});

			const url = testData.testUrl();
			const additionalText = ' Check this out';
			await chatPage.typeMessage(url + additionalText);
			await chatPage.clickAddSource();

			// URL should be removed, additional text should remain
			await expect(chatPage.messageInput).toHaveValue(additionalText.trim());
		});
	});

	test.describe('File Path Detection', () => {
		test('shows suggestion chip when file path is pasted', async ({ chatPage }) => {
			const filePath = testData.testFilePath();
			await chatPage.typeMessage(filePath);

			await chatPage.expectSuggestionChip('file');
			await chatPage.expectSuggestionChipValue('document.md');
		});

		test('detects .txt files', async ({ chatPage }) => {
			const filePath = testData.testFilePathWithExtension('txt');
			await chatPage.typeMessage(filePath);

			await chatPage.expectSuggestionChip('file');
		});

		test('detects .pdf files', async ({ chatPage }) => {
			const filePath = testData.testFilePathWithExtension('pdf');
			await chatPage.typeMessage(filePath);

			await chatPage.expectSuggestionChip('file');
		});

		test('opens upload modal when file chip clicked', async ({ chatPage }) => {
			const filePath = testData.testFilePath();
			await chatPage.typeMessage(filePath);
			await chatPage.expectSuggestionChip('file');

			await chatPage.clickAddSource();
			await chatPage.expectUploadModalVisible();
		});

		test('does not detect file when URL is present', async ({ chatPage }) => {
			// URL takes precedence over file path
			const content = testData.testUrl() + ' /path/to/file.md';
			await chatPage.typeMessage(content);

			await chatPage.expectSuggestionChip('url');
		});
	});

	test.describe('/source Command', () => {
		test('adds URL via /source command', async ({ chatPage, page }) => {
			await page.route('**/sources/web', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						id: 'src-456',
						name: 'Example Docs',
						type: 'web',
						url: testData.testUrl()
					})
				});
			});

			const command = testData.sourceCommand(testData.testUrl());
			await chatPage.sendMessage(command);

			await chatPage.expectStatusMessage('Added source');
		});

		test('shows error for invalid URL in /source command', async ({ chatPage }) => {
			await chatPage.sendMessage('/source not-a-valid-url');

			await chatPage.expectStatusMessage('Invalid URL');
		});

		test('shows error for URL not starting with http', async ({ chatPage }) => {
			await chatPage.sendMessage('/source example.com');

			await chatPage.expectStatusMessage('must start with http');
		});

		test('opens upload modal with /source upload', async ({ chatPage }) => {
			await chatPage.sendMessage(testData.sourceUploadCommand());

			await chatPage.expectUploadModalVisible();
		});

		test('shows usage help for empty /source command', async ({ chatPage }) => {
			// Note: /source needs a trailing space to trigger command handling
			await chatPage.sendMessage('/source ');

			await chatPage.expectStatusMessage('Usage:');
		});
	});

	test.describe('Upload Modal', () => {
		test.beforeEach(async ({ chatPage }) => {
			// Open upload modal via command
			await chatPage.sendMessage('/source upload');
			await chatPage.expectUploadModalVisible();
		});

		test('closes modal with cancel button', async ({ chatPage }) => {
			await chatPage.closeUploadModal();
			await chatPage.expectUploadModalHidden();
		});

		test('shows file drop zone initially', async ({ chatPage }) => {
			await expect(chatPage.uploadModalDropZone).toBeVisible();
			await expect(chatPage.uploadModalDropZone).toContainText('Drag and drop');
		});

		test('shows category selection after file selected', async ({ chatPage }) => {
			// Create a test file
			const testFile = path.join(__dirname, 'fixtures', 'test-document.md');

			// Note: This test requires a fixture file. If it doesn't exist,
			// we can skip or create it dynamically
			await chatPage.uploadFile(testFile);

			// Category buttons should be visible
			await expect(chatPage.uploadModalCategoryButtons.first()).toBeVisible();
		});

		test('disables upload button without file', async ({ chatPage }) => {
			await expect(chatPage.uploadModalUploadButton).toBeDisabled();
		});
	});

	test.describe('Command Hints', () => {
		test('shows /source in command hints', async ({ chatPage }) => {
			// Command hints are shown when message list is empty
			const hintsRegion = chatPage.page.locator('[aria-label="Quick commands"]');
			await expect(hintsRegion).toBeVisible();

			const sourceHint = hintsRegion.locator('code', { hasText: '/source' });
			await expect(sourceHint).toBeVisible();
		});

		test('executes /source command from hint click', async ({ chatPage }) => {
			const hintsRegion = chatPage.page.locator('[aria-label="Quick commands"]');
			const sourceHint = hintsRegion.locator('.hint-chip').filter({ hasText: '/source' });
			await sourceHint.click();

			// Verify hint was clicked by checking input is cleared and hints are hidden
			await expect(chatPage.messageInput).toHaveValue('');
		});
	});

	test.describe('Drag and Drop', () => {
		// Note: Playwright has limited support for drag-and-drop with files
		// These tests verify the visual feedback elements exist

		test('drop zone container exists', async ({ chatPage }) => {
			await expect(chatPage.dropZone).toBeVisible();
		});

		test('drop overlay is hidden by default', async ({ chatPage }) => {
			await chatPage.expectDropOverlayHidden();
		});

		// Drag-and-drop file tests would require DataTransfer API mocking
		// which is complex in Playwright. The component behavior is tested
		// via unit tests instead.
	});

	test.describe('Accessibility', () => {
		test('suggestion chip has correct ARIA attributes', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);

			await expect(chatPage.suggestionChip).toHaveAttribute('role', 'group');
			await expect(chatPage.suggestionChip).toHaveAttribute(
				'aria-label',
				/Source suggestion: URL detected/i
			);
		});

		test('suggestion chip add button has aria-label', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);

			await expect(chatPage.suggestionChipAddButton).toHaveAttribute('aria-label', 'Add as source');
		});

		test('dismiss button has aria-label', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);

			await expect(chatPage.suggestionChipDismissButton).toHaveAttribute(
				'aria-label',
				'Dismiss suggestion'
			);
		});

		test('escape key dismisses suggestion chip', async ({ chatPage }) => {
			const url = testData.testUrl();
			await chatPage.typeMessage(url);
			await chatPage.expectSuggestionChip('url');

			// Focus the dismiss button and press Escape
			await chatPage.suggestionChipDismissButton.focus();
			await chatPage.page.keyboard.press('Escape');

			await chatPage.expectNoSuggestionChip();
		});

		test('upload modal has correct ARIA attributes', async ({ chatPage }) => {
			await chatPage.sendMessage('/source upload');

			await expect(chatPage.uploadModal).toHaveAttribute('role', 'dialog');
			await expect(chatPage.uploadModal).toHaveAttribute('aria-modal', 'true');
			await expect(chatPage.uploadModal).toHaveAttribute('aria-labelledby', 'upload-title');
		});
	});
});
