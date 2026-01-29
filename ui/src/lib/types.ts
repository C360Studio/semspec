export interface Message {
	id: string;
	type: 'user' | 'assistant' | 'status' | 'error';
	content: string;
	timestamp: string;
	loopId?: string;
}

export interface Loop {
	id: string;
	state: LoopState;
	role: string;
	model: string;
	iterations: number;
	maxIterations: number;
	owner?: string;
	source?: string;
	pendingTools?: string[];
	startedAt?: string;
	prompt?: string;
}

export type LoopState =
	| 'executing'
	| 'paused'
	| 'awaiting_approval'
	| 'complete'
	| 'failed'
	| 'cancelled';

export interface ActivityEvent {
	type:
		| 'loop_started'
		| 'loop_complete'
		| 'model_request'
		| 'model_response'
		| 'tool_call'
		| 'tool_result'
		| 'status_update';
	loop_id: string;
	timestamp: string;
	data: Record<string, unknown>;
}

export interface SystemHealth {
	healthy: boolean;
	components: ComponentHealth[];
}

export interface ComponentHealth {
	name: string;
	status: 'running' | 'stopped' | 'error';
	uptime: number;
}
