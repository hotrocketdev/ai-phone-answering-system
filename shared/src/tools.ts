// === Tool Call Types ===

export interface ToolResult {
  success: boolean;
  data?: Record<string, unknown>;
  error?: string;
  alternatives?: string[];
}

export interface ToolCallRequest {
  callSid: string;
  tenantId: string;
  toolName: string;
  arguments: Record<string, unknown>;
  signature: string;
  timestamp: number;
}

export interface CheckAvailabilityArgs {
  date: string;
  time: string;
  partySize: number;
}

export interface CreateBookingArgs {
  date: string;
  time: string;
  partySize: number;
  name: string;
  phone: string;
  email?: string;
  notes?: string;
}
