import type { PlanWithStatus } from './plan';

const TITLE_LIMIT = 96;
const SUMMARY_LIMIT = 220;

export function isRawPromptLike(value: string | null | undefined): boolean {
	const text = value?.trim() ?? '';
	if (!text) return false;
	if (text.length > TITLE_LIMIT) return true;
	if (text.includes('\n')) return true;
	if (/^#+\s/.test(text)) return true;
	if (/\b(must|shall|implement|design)\b/i.test(text) && text.length > 72) return true;
	return false;
}

export function planDisplayTitle(plan: PlanWithStatus | null | undefined): string {
	if (!plan) return 'Plan';
	const title = plan.title?.trim();
	if (title && !isRawPromptLike(title)) return title;

	const goal = plan.goal?.trim();
	if (goal && !isRawPromptLike(goal)) return compactPlanText(goal, TITLE_LIMIT);

	return plan.slug ? `Plan ${plan.slug}` : 'Plan';
}

export function compactPlanText(value: string | null | undefined, limit = SUMMARY_LIMIT): string {
	const text = (value ?? '').replace(/\s+/g, ' ').trim();
	if (text.length <= limit) return text;
	return `${text.slice(0, Math.max(0, limit - 3)).trimEnd()}...`;
}

export function shouldCollapsePlanText(value: string | null | undefined): boolean {
	return isRawPromptLike(value) || (value?.length ?? 0) > SUMMARY_LIMIT;
}
