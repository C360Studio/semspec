/**
 * Loop mutation actions.
 */
import { invalidate } from '$app/navigation';
import { api } from '$lib/api/client';

export async function sendLoopSignal(
	loopId: string,
	type: 'pause' | 'resume' | 'cancel',
	reason?: string
): Promise<void> {
	await api.router.sendSignal(loopId, type, reason);
	await invalidate('app:loops');
}
