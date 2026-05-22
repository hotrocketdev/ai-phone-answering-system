// === Tenant Types ===

export interface TenantConfig {
  tenantId: string;
  restaurantName: string;
  cuisineType: string;
  address: string;
  basePrompt: string;
  voice: string;
  ttsVoiceId?: string;
  businessHours: {
    start: string;
    end: string;
  };
  maxPartySize: number;
  enableSmsConfirmations: boolean;
}

export interface TenantConfigResponse {
  tenant: TenantConfig;
  tools: string[];
}
