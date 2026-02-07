import { api } from '$lib/api/client';
import type { Question, QuestionStatus } from '$lib/types';

/**
 * Questions store for Knowledge Gap Resolution Protocol.
 *
 * Questions are managed via commands (/questions, /answer) through the
 * message interface. This store parses command responses to display
 * question state in the UI.
 */
class QuestionsStore {
	all = $state<Question[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);
	lastRefresh = $state<Date | null>(null);

	get pending(): Question[] {
		return this.all.filter((q) => q.status === 'pending');
	}

	get answered(): Question[] {
		return this.all.filter((q) => q.status === 'answered');
	}

	get timedOut(): Question[] {
		return this.all.filter((q) => q.status === 'timeout');
	}

	get blocking(): Question[] {
		return this.pending.filter((q) => q.urgency === 'blocking');
	}

	/**
	 * Fetch questions by sending /questions command.
	 * Parses the markdown table response to extract question data.
	 */
	async fetch(status?: QuestionStatus | 'all'): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			const command = status ? `/questions ${status}` : '/questions';
			const response = await api.router.sendMessage(command);

			// Parse the markdown table from response
			this.all = this.parseQuestionsResponse(response.content);
			this.lastRefresh = new Date();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch questions';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Answer a question by sending /answer command.
	 */
	async answer(questionId: string, response: string): Promise<void> {
		const command = `/answer ${questionId} "${response.replace(/"/g, '\\"')}"`;
		await api.router.sendMessage(command);

		// Refresh to get updated state
		await this.fetch();
	}

	/**
	 * Ask a new question by sending /ask command.
	 */
	async ask(topic: string, question: string): Promise<string | null> {
		const command = `/ask ${topic} "${question.replace(/"/g, '\\"')}"`;
		const response = await api.router.sendMessage(command);

		// Extract question ID from response (format: "Created question **q-xxxxx**")
		const match = response.content.match(/\*\*([qQ]-[\w]+)\*\*/);

		// Refresh to show new question
		await this.fetch();

		return match ? match[1] : null;
	}

	/**
	 * Parse markdown table response from /questions command.
	 * Table format: | ID | Topic | Status | Question |
	 */
	private parseQuestionsResponse(content: string): Question[] {
		const questions: Question[] = [];

		// Check for "No X questions found" message
		if (content.includes('No') && content.includes('questions found')) {
			return [];
		}

		// Split into lines and find table rows
		const lines = content.split('\n');
		let inTable = false;

		for (const line of lines) {
			// Detect table start
			if (line.startsWith('| ID |')) {
				inTable = true;
				continue;
			}

			// Skip header separator
			if (line.startsWith('|---')) {
				continue;
			}

			// Parse table rows
			if (inTable && line.startsWith('|')) {
				const cells = line
					.split('|')
					.map((c) => c.trim())
					.filter((c) => c);

				if (cells.length >= 4) {
					questions.push({
						id: cells[0],
						topic: cells[1],
						status: cells[2] as QuestionStatus,
						question: cells[3],
						from_agent: 'unknown',
						urgency: 'normal',
						created_at: new Date().toISOString()
					});
				}
			}

			// Table ends at separator or empty line
			if (inTable && (line.startsWith('---') || line.trim() === '')) {
				inTable = false;
			}
		}

		return questions;
	}

	/**
	 * Get a single question's full details by ID.
	 * Sends /questions <id> command and parses detailed response.
	 */
	async getQuestion(id: string): Promise<Question | null> {
		try {
			const response = await api.router.sendMessage(`/questions ${id}`);
			return this.parseQuestionDetail(response.content, id);
		} catch {
			return null;
		}
	}

	/**
	 * Parse detailed question view response.
	 */
	private parseQuestionDetail(content: string, id: string): Question | null {
		// Check for error
		if (content.includes('not found')) {
			return null;
		}

		const question: Question = {
			id,
			from_agent: '',
			topic: '',
			question: '',
			urgency: 'normal',
			status: 'pending',
			created_at: new Date().toISOString()
		};

		// Parse markdown fields
		const statusMatch = content.match(/\*\*Status\*\*:\s*(\w+)/);
		if (statusMatch) question.status = statusMatch[1] as QuestionStatus;

		const topicMatch = content.match(/\*\*Topic\*\*:\s*(.+)/);
		if (topicMatch) question.topic = topicMatch[1].trim();

		const fromMatch = content.match(/\*\*From\*\*:\s*(.+)/);
		if (fromMatch) question.from_agent = fromMatch[1].trim();

		const createdMatch = content.match(/\*\*Created\*\*:\s*(.+)/);
		if (createdMatch) question.created_at = createdMatch[1].trim();

		const urgencyMatch = content.match(/\*\*Urgency\*\*:\s*(\w+)/);
		if (urgencyMatch) question.urgency = urgencyMatch[1] as Question['urgency'];

		// Parse question text (after ## Question section)
		const questionSection = content.match(/## Question\n\n([\s\S]*?)\n\n/);
		if (questionSection) question.question = questionSection[1].trim();

		// Parse context if present
		const contextSection = content.match(/## Context\n\n([\s\S]*?)\n\n/);
		if (contextSection) question.context = contextSection[1].trim();

		// Parse answer if present
		const answerSection = content.match(/## Answer\n\n([\s\S]*?)\n\n/);
		if (answerSection) question.answer = answerSection[1].trim();

		const answeredByMatch = content.match(/\*\*Answered by\*\*:\s*(\S+)\s*\((\w+)\)/);
		if (answeredByMatch) {
			question.answered_by = answeredByMatch[1];
			question.answerer_type = answeredByMatch[2] as Question['answerer_type'];
		}

		const answeredAtMatch = content.match(/\*\*Answered at\*\*:\s*(.+)/);
		if (answeredAtMatch) question.answered_at = answeredAtMatch[1].trim();

		const confidenceMatch = content.match(/\*\*Confidence\*\*:\s*(\w+)/);
		if (confidenceMatch) question.confidence = confidenceMatch[1] as Question['confidence'];

		const sourcesMatch = content.match(/\*\*Sources\*\*:\s*(.+)/);
		if (sourcesMatch) question.sources = sourcesMatch[1].trim();

		return question;
	}
}

export const questionsStore = new QuestionsStore();
