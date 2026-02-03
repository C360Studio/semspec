// UI-only message type for chat display
export interface Message {
	id: string;
	type: 'user' | 'assistant' | 'status' | 'error';
	content: string;
	timestamp: string;
	loopId?: string;
}

// Backend LoopInfo struct (snake_case from JSON)
export interface Loop {
	loop_id: string;
	task_id: string;
	user_id: string;
	channel_type: string;
	channel_id: string;
	state: LoopState;
	iterations: number;
	max_iterations: number;
	created_at: string;
}

export type LoopState = 'pending' | 'executing' | 'paused' | 'complete' | 'failed' | 'cancelled';

// Activity events from SSE (matches backend ActivityEvent)
export interface ActivityEvent {
	type: 'loop_created' | 'loop_updated' | 'loop_deleted';
	loop_id: string;
	timestamp: string;
	data?: Record<string, unknown>;
}

// Signal request/response
export interface SignalRequest {
	type: 'pause' | 'resume' | 'cancel';
	reason?: string;
}

export interface SignalResponse {
	loop_id: string;
	signal: string;
	accepted: boolean;
	message?: string;
	timestamp: string;
}

// Message response from backend (HTTPMessageResponse)
export interface MessageResponse {
	response_id: string;
	type: string;
	content: string;
	in_reply_to?: string;
	error?: string;
	timestamp: string;
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
