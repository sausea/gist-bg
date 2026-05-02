import type { ContentType } from "./api";

export type AIProvider = "openai" | "anthropic" | "compatible";

export type ReasoningEffort =
  | "low"
  | "medium"
  | "high"
  | "xhigh"
  | "minimal"
  | "none"
  | "";

export type OpenAIEndpoint = "responses" | "chat/completions";

export interface AIModelSettings {
  provider: AIProvider;
  apiKey: string;
  baseUrl: string;
  model: string;
  endpoint: OpenAIEndpoint;
  thinking: boolean;
  thinkingBudget: number;
  reasoningEffort: ReasoningEffort;
}

export interface AISettings {
  analysis: AIModelSettings;
  translation: AIModelSettings;
  report: AIModelSettings;
  summaryLanguage: string;
  autoTranslate: boolean;
  autoTranslateTitle: boolean;
  autoAnalysis: boolean;
  rateLimit: number;
  workerCount: number;
}

export interface AIUsageCounter {
  requestCount: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
}

export interface AIUsageSceneStats extends AIUsageCounter {
  scene: "analysis" | "translation" | "report";
}

export interface AIUsagePeriodStats extends AIUsageCounter {
  scenes: AIUsageSceneStats[];
}

export interface AIUsageDayStats extends AIUsagePeriodStats {
  date: string;
}

export interface AIUsageStats {
  today: AIUsagePeriodStats;
  last7Days: AIUsagePeriodStats;
  allTime: AIUsagePeriodStats;
  daily: AIUsageDayStats[];
}

export interface AIPromptTemplate {
  key: string;
  fileName: string;
  variables: string[];
  content: string;
  defaultContent: string;
}

export interface AIPromptSettings {
  dir: string;
  templates: AIPromptTemplate[];
}

export interface AITestRequest {
  provider: AIProvider;
  apiKey: string;
  baseUrl: string;
  model: string;
  endpoint: OpenAIEndpoint;
  thinking: boolean;
  thinkingBudget: number;
  reasoningEffort: ReasoningEffort;
}

export interface AITestResponse {
  success: boolean;
  message?: string;
  error?: string;
}

export interface GeneralSettings {
  fallbackUserAgent: string;
  autoReadability: boolean;
  aiDailyReportApiKey: string;
  aiAnalysisArchiveDir: string;
}

export type ProxyType = "http" | "socks5";

export type IPStack = "default" | "ipv4" | "ipv6";

export interface NetworkSettings {
  enabled: boolean;
  type: ProxyType;
  host: string;
  port: number;
  username: string;
  password: string;
  ipStack: IPStack;
}

export interface NetworkTestRequest {
  enabled: boolean;
  type: ProxyType;
  host: string;
  port: number;
  username: string;
  password: string;
}

export interface NetworkTestResponse {
  success: boolean;
  message?: string;
  error?: string;
}

export interface DomainRateLimit {
  id: string;
  host: string;
  intervalSeconds: number;
}

export interface DomainRateLimitListResponse {
  items: DomainRateLimit[];
}

export interface AppearanceSettings {
  contentTypes: ContentType[];
}
