// === Session Types ===

export type MetaState =
  | 'CREATED'
  | 'CONNECTING'
  | 'ACTIVE'
  | 'RECONNECTING'
  | 'ENDING'
  | 'CLEANUP';

export type ConversationState =
  | 'GREETING'
  | 'FAQ_ANSWER'
  | 'COLLECT_BOOKING_DETAILS'
  | 'CHECK_AVAILABILITY'
  | 'CONFIRM_BOOKING'
  | 'MODIFY_RESERVATION'
  | 'CANCEL_RESERVATION'
  | 'HUMAN_TRANSFER'
  | 'HANDLE_UNAVAILABLE'
  | 'CLOSING';

export interface ConversationTurn {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

export interface ToolCallRecord {
  tool: string;
  args: Record<string, unknown>;
  result: ToolResult;
  timestamp: string;
}

export interface SessionState {
  callSid: string;
  tenantId: string;
  restaurantId: string;
  phoneFrom: string;
  phoneTo: string;
  metaState: MetaState;
  convState: ConversationState;
  conversationHistory: ConversationTurn[];
  toolCallHistory: ToolCallRecord[];
  inputAudioSecs: number;
  outputAudioSecs: number;
  textTokensIn: number;
  textTokensOut: number;
  createdAt: string;
  lastActivity: string;
}
