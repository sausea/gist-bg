import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  getAISettings,
  getAIUsageStats,
  getAIPromptSettings,
  updateAISettings,
  updateAIPromptSettings,
  testAIConnection,
  ApiError,
} from "@/api";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import type {
  AIModelSettings,
  AIPromptSettings as AIPromptSettingsType,
  AIPromptTemplate,
  AIProvider,
  AISettings as AISettingsType,
  AIUsageStats,
  OpenAIEndpoint,
  ReasoningEffort,
} from "@/types/settings";

type ModelSectionKey = "analysis" | "translation" | "report";
type PromptTemplateKey =
  | "summary"
  | "translate_block"
  | "translate_text"
  | "analysis"
  | "daily_report"
  | "coordinate_lookup";

interface ModelSectionMeta {
  key: ModelSectionKey;
  title: string;
  description: string;
}

interface PromptTemplateMeta {
  title: string;
  description: string;
}

interface AIModelSectionProps {
  meta: ModelSectionMeta;
  settings: AIModelSettings;
  isTesting: boolean;
  testResult: { success: boolean; message?: string; error?: string } | null;
  onChange: (field: keyof AIModelSettings, value: string | boolean | number) => void;
  onMultiChange: (changes: Partial<AIModelSettings>) => void;
  onTest: () => Promise<void>;
  t: (key: string, options?: Record<string, unknown>) => string;
  providers: { value: AIProvider; label: string }[];
  endpointOptions: { value: OpenAIEndpoint; label: string }[];
  openAIEffortOptions: { value: ReasoningEffort; label: string }[];
  compatibleEffortOptions: { value: ReasoningEffort; label: string }[];
}

function formatUsageNumber(value: number) {
  return new Intl.NumberFormat().format(value);
}

function getUsageSceneLabel(
  scene: "analysis" | "translation" | "report",
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  switch (scene) {
    case "translation":
      return t("ai_settings.usage_scene_translation");
    case "report":
      return t("ai_settings.usage_scene_report");
    default:
      return t("ai_settings.usage_scene_analysis");
  }
}

function AIModelSection({
  meta,
  settings,
  isTesting,
  testResult,
  onChange,
  onMultiChange,
  onTest,
  t,
  providers,
  endpointOptions,
  openAIEffortOptions,
  compatibleEffortOptions,
}: AIModelSectionProps) {
  const selectClass =
    "h-9 w-full sm:w-48 rounded-md border border-border bg-background px-3 text-sm focus:border-primary focus:outline-none";
  const inputClass =
    "h-9 w-full sm:w-48 rounded-md border border-border bg-background px-3 text-sm focus:border-primary focus:outline-none";

  return (
    <section className="space-y-1 rounded-xl border border-border/70 bg-card/30 p-4">
      <div className="pb-2">
        <h3 className="text-sm font-semibold text-foreground">{meta.title}</h3>
        <p className="mt-1 text-xs text-muted-foreground">{meta.description}</p>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 py-2">
        <span className="text-sm font-medium">{t("ai_settings.provider")}</span>
        <select
          value={settings.provider}
          onChange={(e) => onChange("provider", e.target.value)}
          className={cn(selectClass, "shrink-0")}
        >
          {providers.map((provider) => (
            <option key={`${meta.key}-${provider.value}`} value={provider.value}>
              {provider.label}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 py-2">
        <span className="text-sm font-medium">{t("ai_settings.api_key")}</span>
        <input
          type="password"
          value={settings.apiKey}
          onChange={(e) => onChange("apiKey", e.target.value)}
          placeholder={
            settings.provider === "openai"
              ? "sk-..."
              : settings.provider === "anthropic"
                ? "sk-ant-..."
                : t("ai_settings.enter_api_key")
          }
          className={cn(inputClass, "shrink-0")}
        />
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 py-2">
        <div className="flex min-w-0 items-center gap-1">
          <span className="text-sm font-medium">{t("ai_settings.base_url")}</span>
          {settings.provider === "compatible" ? (
            <span className="text-xs text-destructive">{t("ai_settings.required")}</span>
          ) : (
            <span className="text-xs text-muted-foreground">{t("ai_settings.optional")}</span>
          )}
        </div>
        <input
          type="text"
          value={settings.baseUrl}
          onChange={(e) => onChange("baseUrl", e.target.value)}
          placeholder={
            settings.provider === "compatible"
              ? "https://openrouter.ai/api/v1"
              : t("ai_settings.leave_empty_for_default")
          }
          className={cn(inputClass, "shrink-0")}
        />
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 py-2">
        <span className="text-sm font-medium">{t("ai_settings.model")}</span>
        <input
          type="text"
          value={settings.model}
          onChange={(e) => onChange("model", e.target.value)}
          placeholder={
            settings.provider === "openai"
              ? "gpt-4o"
              : settings.provider === "anthropic"
                ? "claude-sonnet-4-20250514"
                : t("ai_settings.model_example", { example: "anthropic/claude-3.5-sonnet" })
          }
          className={cn(inputClass, "shrink-0")}
        />
      </div>

      {settings.provider === "openai" && (
        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <span className="text-sm font-medium">{t("ai_settings.endpoint_label")}</span>
          <select
            value={settings.endpoint}
            onChange={(e) => onChange("endpoint", e.target.value as OpenAIEndpoint)}
            className={cn(selectClass, "shrink-0")}
          >
            {endpointOptions.map((option) => (
              <option key={`${meta.key}-${option.value}`} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
      )}

      <div className="pb-1 pt-4 text-xs font-medium uppercase tracking-wider text-muted-foreground">
        {t("ai_settings.extended_thinking")}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 py-2">
        <div className="min-w-0">
          <span className="text-sm font-medium">
            {settings.provider === "anthropic"
              ? t("ai_settings.extended_thinking")
              : t("ai_settings.enable_reasoning")}
          </span>
          {settings.provider === "openai" && !settings.thinking && (
            <p className="text-xs text-muted-foreground">
              {t("ai_settings.openai_thinking_default_hint")}
            </p>
          )}
        </div>
        <Switch checked={settings.thinking} onCheckedChange={(checked) => onChange("thinking", checked)} />
      </div>

      {settings.thinking && settings.provider === "openai" && (
        <div className="flex flex-wrap items-center justify-between gap-2 py-2 pl-4">
          <span className="text-sm">{t("ai_settings.reasoning_effort_label")}</span>
          <select
            value={settings.reasoningEffort}
            onChange={(e) => onChange("reasoningEffort", e.target.value)}
            className={cn(selectClass, "shrink-0")}
          >
            {openAIEffortOptions.map((option) => (
              <option key={`${meta.key}-${option.value}`} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
      )}

      {settings.thinking && settings.provider === "anthropic" && (
        <div className="flex flex-wrap items-center justify-between gap-2 py-2 pl-4">
          <div className="min-w-0">
            <span className="text-sm">{t("ai_settings.thinking_budget_label")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.thinking_budget_hint")}</p>
          </div>
          <input
            type="number"
            value={settings.thinkingBudget}
            onChange={(e) => onChange("thinkingBudget", parseInt(e.target.value, 10) || 0)}
            min={1024}
            max={128000}
            placeholder="10000"
            className={cn(inputClass, "w-24 shrink-0")}
          />
        </div>
      )}

      {settings.thinking && settings.provider === "compatible" && (
        <div className="space-y-2 pl-4">
          <div className="flex flex-wrap items-center justify-between gap-2 py-1">
            <div className="flex min-w-0 items-center gap-2">
              <input
                type="radio"
                id={`${meta.key}-compatible-effort`}
                name={`${meta.key}-compatible-mode`}
                checked={settings.reasoningEffort !== ""}
                onChange={() => onMultiChange({ reasoningEffort: "medium", thinkingBudget: 0 })}
                className="size-4 shrink-0"
              />
              <label htmlFor={`${meta.key}-compatible-effort`} className="text-sm">
                {t("ai_settings.reasoning_effort_mode")}
              </label>
              <span className="text-xs text-muted-foreground">{t("ai_settings.o1_grok_models")}</span>
            </div>
            {settings.reasoningEffort !== "" && (
              <select
                value={settings.reasoningEffort}
                onChange={(e) => onChange("reasoningEffort", e.target.value)}
                className={cn(selectClass, "w-32 shrink-0")}
              >
                {compatibleEffortOptions.map((option) => (
                  <option key={`${meta.key}-${option.value}`} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 py-1">
            <div className="flex min-w-0 items-center gap-2">
              <input
                type="radio"
                id={`${meta.key}-compatible-budget`}
                name={`${meta.key}-compatible-mode`}
                checked={settings.reasoningEffort === "" && settings.thinkingBudget > 0}
                onChange={() => onMultiChange({ reasoningEffort: "", thinkingBudget: 10000 })}
                className="size-4 shrink-0"
              />
              <label htmlFor={`${meta.key}-compatible-budget`} className="text-sm">
                {t("ai_settings.thinking_budget_mode")}
              </label>
              <span className="text-xs text-muted-foreground">{t("ai_settings.anthropic_gemini_models")}</span>
            </div>
            {settings.reasoningEffort === "" && settings.thinkingBudget > 0 && (
              <input
                type="number"
                value={settings.thinkingBudget}
                onChange={(e) => onChange("thinkingBudget", parseInt(e.target.value, 10) || 0)}
                min={1024}
                max={128000}
                placeholder="10000"
                className={cn(inputClass, "w-24 shrink-0")}
              />
            )}
          </div>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3 pt-4">
        <button
          type="button"
          onClick={onTest}
          disabled={
            isTesting ||
            !settings.apiKey ||
            !settings.model ||
            (settings.provider === "compatible" && !settings.baseUrl)
          }
          className={cn(
            "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-4 text-sm font-medium transition-colors",
            "bg-muted hover:bg-muted/80",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          {isTesting ? (
            <>
              <div className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
              <span>{t("ai_settings.testing")}</span>
            </>
          ) : (
            <>
              <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M13 10V3L4 14h7v7l9-11h-7z"
                />
              </svg>
              <span>{t("ai_settings.test")}</span>
            </>
          )}
        </button>

        {testResult && (
          <div
            className={cn(
              "text-sm",
              testResult.success ? "text-green-600" : "text-destructive",
            )}
          >
            {testResult.success
              ? testResult.message || t("ai_settings.test_success")
              : testResult.error || t("ai_settings.test_failed")}
          </div>
        )}
      </div>
    </section>
  );
}

export function AISettings() {
  const { t } = useTranslation();

  const providers: { value: AIProvider; label: string }[] = useMemo(
    () => [
      { value: "openai", label: t("ai_settings.provider_openai") },
      { value: "anthropic", label: t("ai_settings.provider_anthropic") },
      { value: "compatible", label: t("ai_settings.provider_compatible") },
    ],
    [t],
  );

  const openAIEffortOptions: { value: ReasoningEffort; label: string }[] = useMemo(
    () => [
      { value: "xhigh", label: t("ai_settings.effort_xhigh") },
      { value: "high", label: t("ai_settings.effort_high") },
      { value: "medium", label: t("ai_settings.effort_medium") },
      { value: "low", label: t("ai_settings.effort_low") },
      { value: "minimal", label: t("ai_settings.effort_minimal") },
      { value: "none", label: t("ai_settings.effort_none_gpt5") },
    ],
    [t],
  );

  const compatibleEffortOptions: { value: ReasoningEffort; label: string }[] = useMemo(
    () => [
      { value: "xhigh", label: t("ai_settings.effort_xhigh_percent") },
      { value: "high", label: t("ai_settings.effort_high_percent") },
      { value: "medium", label: t("ai_settings.effort_medium_percent") },
      { value: "low", label: t("ai_settings.effort_low_percent") },
      { value: "minimal", label: t("ai_settings.effort_minimal_percent") },
      { value: "none", label: t("ai_settings.effort_none") },
    ],
    [t],
  );

  const endpointOptions: { value: OpenAIEndpoint; label: string }[] = useMemo(
    () => [
      { value: "responses", label: t("ai_settings.endpoint_responses") },
      { value: "chat/completions", label: t("ai_settings.endpoint_chat") },
    ],
    [t],
  );

  const summaryLanguageOptions: { value: string; label: string }[] = useMemo(
    () => [
      { value: "zh-CN", label: t("ai_settings.lang_zh_cn") },
      { value: "zh-TW", label: t("ai_settings.lang_zh_tw") },
      { value: "en-US", label: t("ai_settings.lang_en") },
      { value: "ja", label: t("ai_settings.lang_ja") },
      { value: "ko", label: t("ai_settings.lang_ko") },
      { value: "es", label: t("ai_settings.lang_es") },
      { value: "fr", label: t("ai_settings.lang_fr") },
      { value: "de", label: t("ai_settings.lang_de") },
    ],
    [t],
  );

  const modelSections: ModelSectionMeta[] = useMemo(
    () => [
      {
        key: "analysis",
        title: t("ai_settings.analysis_model_title"),
        description: t("ai_settings.analysis_model_hint"),
      },
      {
        key: "translation",
        title: t("ai_settings.translation_model_title"),
        description: t("ai_settings.translation_model_hint"),
      },
      {
        key: "report",
        title: t("ai_settings.report_model_title"),
        description: t("ai_settings.report_model_hint"),
      },
    ],
    [t],
  );

  const promptTemplateMeta = useMemo<Record<PromptTemplateKey, PromptTemplateMeta>>(
    () => ({
      summary: {
        title: t("ai_settings.prompt_template_summary_title"),
        description: t("ai_settings.prompt_template_summary_hint"),
      },
      translate_block: {
        title: t("ai_settings.prompt_template_translate_block_title"),
        description: t("ai_settings.prompt_template_translate_block_hint"),
      },
      translate_text: {
        title: t("ai_settings.prompt_template_translate_text_title"),
        description: t("ai_settings.prompt_template_translate_text_hint"),
      },
      analysis: {
        title: t("ai_settings.prompt_template_analysis_title"),
        description: t("ai_settings.prompt_template_analysis_hint"),
      },
      daily_report: {
        title: t("ai_settings.prompt_template_daily_report_title"),
        description: t("ai_settings.prompt_template_daily_report_hint"),
      },
      coordinate_lookup: {
        title: t("ai_settings.prompt_template_coordinate_lookup_title"),
        description: t("ai_settings.prompt_template_coordinate_lookup_hint"),
      },
    }),
    [t],
  );

  const [settings, setSettings] = useState<AISettingsType | null>(null);
  const [promptSettings, setPromptSettings] = useState<AIPromptSettingsType | null>(null);
  const [promptDrafts, setPromptDrafts] = useState<Record<string, string>>({});
  const [activePromptKey, setActivePromptKey] = useState<PromptTemplateKey>("analysis");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isPromptSaving, setIsPromptSaving] = useState(false);
  const [testingSection, setTestingSection] = useState<ModelSectionKey | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [promptError, setPromptError] = useState<string | null>(null);
  const [promptSuccessMessage, setPromptSuccessMessage] = useState<string | null>(null);
  const [testTarget, setTestTarget] = useState<ModelSectionKey | null>(null);
  const [testResult, setTestResult] = useState<{ success: boolean; message?: string; error?: string } | null>(null);
  const [usageDays, setUsageDays] = useState(30);
  const [usageStats, setUsageStats] = useState<AIUsageStats | null>(null);
  const [usageError, setUsageError] = useState<string | null>(null);
  const [isUsageLoading, setIsUsageLoading] = useState(false);

  useEffect(() => {
    void loadSettings();
  }, []);

  const syncPromptSettings = (next: AIPromptSettingsType) => {
    setPromptSettings(next);
    setPromptDrafts(
      Object.fromEntries(next.templates.map((template) => [template.key, template.content])),
    );
    setActivePromptKey((current) => {
      if (next.templates.some((template) => template.key === current)) {
        return current;
      }
      const fallback = next.templates[0]?.key;
      return (fallback as PromptTemplateKey | undefined) ?? "analysis";
    });
  };

  const loadUsageStats = async (days: number) => {
    setIsUsageLoading(true);
    setUsageError(null);
    try {
      const stats = await getAIUsageStats(days);
      setUsageStats(stats);
    } catch (err) {
      if (err instanceof ApiError) {
        setUsageError(err.message);
      } else {
        setUsageError(t("ai_settings.usage_failed_to_load"));
      }
    } finally {
      setIsUsageLoading(false);
    }
  };

  const loadSettings = async () => {
    setIsLoading(true);
    setError(null);
    setUsageError(null);
    setPromptError(null);
    setIsUsageLoading(true);
    try {
      const [settingsResult, usageResult, promptsResult] = await Promise.allSettled([
        getAISettings(),
        getAIUsageStats(usageDays),
        getAIPromptSettings(),
      ]);

      if (settingsResult.status === "fulfilled") {
        setSettings(settingsResult.value);
      } else {
        throw settingsResult.reason;
      }

      if (usageResult.status === "fulfilled") {
        setUsageStats(usageResult.value);
      } else if (usageResult.reason instanceof ApiError) {
        setUsageError(usageResult.reason.message);
      } else {
        setUsageError(t("ai_settings.usage_failed_to_load"));
      }

      if (promptsResult.status === "fulfilled") {
        syncPromptSettings(promptsResult.value);
      } else if (promptsResult.reason instanceof ApiError) {
        setPromptError(promptsResult.reason.message);
      } else {
        setPromptError(t("ai_settings.prompt_templates_load_failed"));
      }
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("ai_settings.failed_to_load"));
      }
    } finally {
      setIsUsageLoading(false);
      setIsLoading(false);
    }
  };

  const handleModelChange = (
    section: ModelSectionKey,
    field: keyof AIModelSettings,
    value: string | boolean | number,
  ) => {
    if (!settings) return;

    const nextSection = {
      ...settings[section],
      [field]: value,
    } as AIModelSettings;

    if (field === "provider" && value !== "openai") {
      nextSection.endpoint = "responses";
    }

    setSettings({
      ...settings,
      [section]: nextSection,
    });
    setSuccessMessage(null);
    setTestResult(null);
  };

  const handleModelMultiChange = (section: ModelSectionKey, changes: Partial<AIModelSettings>) => {
    if (!settings) return;
    setSettings({
      ...settings,
      [section]: {
        ...settings[section],
        ...changes,
      },
    });
    setSuccessMessage(null);
    setTestResult(null);
  };

  const handleChange = (field: keyof AISettingsType, value: string | boolean | number) => {
    if (!settings) return;
    setSettings({
      ...settings,
      [field]: value,
    } as AISettingsType);
    setSuccessMessage(null);
    setTestResult(null);
  };

  const handleTest = async (section: ModelSectionKey) => {
    if (!settings) return;
    setTestingSection(section);
    setTestTarget(section);
    setTestResult(null);

    try {
      const target = settings[section];
      const result = await testAIConnection({
        provider: target.provider,
        apiKey: target.apiKey,
        baseUrl: target.baseUrl,
        model: target.model,
        endpoint: target.endpoint,
        thinking: target.thinking,
        thinkingBudget: target.thinkingBudget,
        reasoningEffort: target.reasoningEffort,
      });
      setTestResult(result);
    } catch (err) {
      setTestResult({
        success: false,
        error: err instanceof Error ? err.message : "Test failed",
      });
    } finally {
      setTestingSection(null);
    }
  };

  const handleSave = async () => {
    if (!settings) return;
    setIsSaving(true);
    setError(null);
    setSuccessMessage(null);
    try {
      await updateAISettings(settings);
      setSuccessMessage(t("ai_settings.settings_saved"));
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("ai_settings.failed_to_save"));
      }
    } finally {
      setIsSaving(false);
    }
  };

  const handlePromptChange = (key: string, value: string) => {
    setPromptDrafts((current) => ({
      ...current,
      [key]: value,
    }));
    setPromptError(null);
    setPromptSuccessMessage(null);
  };

  const handlePromptRestoreDefault = (template: AIPromptTemplate) => {
    handlePromptChange(template.key, template.defaultContent);
  };

  const handlePromptSave = async () => {
    if (!promptSettings) return;
    setIsPromptSaving(true);
    setPromptError(null);
    setPromptSuccessMessage(null);
    try {
      const next = await updateAIPromptSettings({
        ...promptSettings,
        templates: promptSettings.templates.map((template) => ({
          ...template,
          content: promptDrafts[template.key] ?? template.content,
        })),
      });
      syncPromptSettings(next);
      setPromptSuccessMessage(t("ai_settings.prompt_templates_saved"));
    } catch (err) {
      if (err instanceof ApiError) {
        setPromptError(err.message);
      } else {
        setPromptError(t("ai_settings.prompt_templates_save_failed"));
      }
    } finally {
      setIsPromptSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-40 items-center justify-center">
        <div className="size-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  if (!settings) {
    return (
      <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
        {error || t("ai_settings.failed_to_load")}
      </div>
    );
  }

  const selectClass =
    "h-9 w-full sm:w-48 rounded-md border border-border bg-background px-3 text-sm focus:border-primary focus:outline-none";
  const inputClass =
    "h-9 w-full sm:w-48 rounded-md border border-border bg-background px-3 text-sm focus:border-primary focus:outline-none";
  const sceneStats = (["analysis", "translation", "report"] as const).map((scene) => {
    const stat = usageStats?.today.scenes.find((item) => item.scene === scene);
    return {
      scene,
      requestCount: stat?.requestCount ?? 0,
      promptTokens: stat?.promptTokens ?? 0,
      completionTokens: stat?.completionTokens ?? 0,
      totalTokens: stat?.totalTokens ?? 0,
    };
  });
  const promptTemplates = promptSettings?.templates ?? [];
  const activePrompt =
    promptTemplates.find((template) => template.key === activePromptKey) ?? promptTemplates[0] ?? null;
  const activePromptContent = activePrompt ? promptDrafts[activePrompt.key] ?? activePrompt.content : "";
  const promptHasChanges = promptTemplates.some(
    (template) => (promptDrafts[template.key] ?? template.content) !== template.content,
  );

  return (
    <div className="space-y-5">
      {modelSections.map((section) => (
        <AIModelSection
          key={section.key}
          meta={section}
          settings={settings[section.key]}
          isTesting={testingSection === section.key}
          testResult={testTarget === section.key ? testResult : null}
          onChange={(field, value) => handleModelChange(section.key, field, value)}
          onMultiChange={(changes) => handleModelMultiChange(section.key, changes)}
          onTest={() => handleTest(section.key)}
          t={t}
          providers={providers}
          endpointOptions={endpointOptions}
          openAIEffortOptions={openAIEffortOptions}
          compatibleEffortOptions={compatibleEffortOptions}
        />
      ))}

      <section className="space-y-1 rounded-xl border border-border/70 bg-card/30 p-4">
        <div className="pb-1 pt-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          AI
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.summary_language")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.summary_language_hint")}</p>
          </div>
          <select
            value={settings.summaryLanguage}
            onChange={(e) => handleChange("summaryLanguage", e.target.value)}
            className={cn(selectClass, "w-40 shrink-0")}
          >
            {summaryLanguageOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.auto_translate_title")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.auto_translate_title_hint")}</p>
          </div>
          <Switch
            checked={settings.autoTranslateTitle}
            onCheckedChange={(checked) => handleChange("autoTranslateTitle", checked)}
            className="shrink-0"
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.auto_translate")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.auto_translate_hint")}</p>
          </div>
          <Switch
            checked={settings.autoTranslate}
            onCheckedChange={(checked) => handleChange("autoTranslate", checked)}
            className="shrink-0"
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.auto_analysis")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.auto_analysis_hint")}</p>
          </div>
          <Switch
            checked={settings.autoAnalysis}
            onCheckedChange={(checked) => handleChange("autoAnalysis", checked)}
            className="shrink-0"
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.rate_limit_label")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.rate_limit_hint")}</p>
          </div>
          <input
            type="number"
            value={settings.rateLimit}
            onChange={(e) => handleChange("rateLimit", parseInt(e.target.value, 10) || 10)}
            min={1}
            max={100}
            className={cn(inputClass, "w-20 shrink-0")}
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 py-2">
          <div className="min-w-0">
            <span className="text-sm font-medium">{t("ai_settings.worker_count_label")}</span>
            <p className="text-xs text-muted-foreground">{t("ai_settings.worker_count_hint")}</p>
          </div>
          <input
            type="number"
            value={settings.workerCount}
            onChange={(e) => handleChange("workerCount", parseInt(e.target.value, 10) || 2)}
            min={1}
            max={16}
            className={cn(inputClass, "w-20 shrink-0")}
          />
        </div>
      </section>

      <section className="space-y-4 rounded-xl border border-border/70 bg-card/30 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="pb-1">
            <h3 className="text-sm font-semibold text-foreground">{t("ai_settings.prompt_templates_title")}</h3>
            <p className="mt-1 text-xs text-muted-foreground">{t("ai_settings.prompt_templates_hint")}</p>
          </div>

          {promptSettings?.dir && (
            <div className="max-w-full rounded-md border border-border/70 bg-background/70 px-3 py-2 text-xs text-muted-foreground">
              <div className="font-medium text-foreground">{t("ai_settings.prompt_templates_dir_label")}</div>
              <div className="mt-1 break-all font-mono">{promptSettings.dir}</div>
            </div>
          )}
        </div>

        {promptError && (
          <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {promptError}
          </div>
        )}

        {promptTemplates.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border/70 bg-background/40 px-4 py-5 text-sm text-muted-foreground">
            {t("ai_settings.prompt_templates_empty")}
          </div>
        ) : (
          <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
            <div className="space-y-2">
              {promptTemplates.map((template) => {
                const meta = promptTemplateMeta[template.key as PromptTemplateKey];
                const isActive = activePrompt?.key === template.key;
                const isDirty = (promptDrafts[template.key] ?? template.content) !== template.content;
                return (
                  <button
                    key={template.key}
                    type="button"
                    onClick={() => setActivePromptKey(template.key as PromptTemplateKey)}
                    className={cn(
                      "w-full rounded-xl border px-3 py-3 text-left transition-colors",
                      isActive
                        ? "border-primary bg-primary/5"
                        : "border-border/70 bg-background/60 hover:bg-background",
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="text-sm font-medium text-foreground">
                        {meta?.title ?? template.key}
                      </div>
                      {isDirty && (
                        <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-700">
                          {t("ai_settings.prompt_templates_dirty")}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">{template.fileName}</div>
                  </button>
                );
              })}
            </div>

            {activePrompt && (
              <div className="space-y-3">
                <div className="space-y-1">
                  <h4 className="text-sm font-semibold text-foreground">
                    {promptTemplateMeta[activePrompt.key as PromptTemplateKey]?.title ?? activePrompt.key}
                  </h4>
                  <p className="text-xs text-muted-foreground">
                    {promptTemplateMeta[activePrompt.key as PromptTemplateKey]?.description}
                  </p>
                  <div className="text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{t("ai_settings.prompt_templates_file_label")}</span>
                    {" "}
                    <span className="font-mono">{activePrompt.fileName}</span>
                  </div>
                </div>

                <div className="rounded-md border border-border/70 bg-background/60 px-3 py-2 text-xs text-muted-foreground">
                  <div className="font-medium text-foreground">{t("ai_settings.prompt_templates_variables_label")}</div>
                  {activePrompt.variables.length > 0 ? (
                    <div className="mt-2 flex flex-wrap gap-2">
                      {activePrompt.variables.map((variable) => (
                        <span
                          key={`${activePrompt.key}-${variable}`}
                          className="rounded-full border border-border/70 bg-card px-2 py-0.5 font-mono text-[11px]"
                        >
                          {variable}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <div className="mt-2">{t("ai_settings.prompt_templates_variables_empty")}</div>
                  )}
                </div>

                <textarea
                  value={activePromptContent}
                  onChange={(e) => handlePromptChange(activePrompt.key, e.target.value)}
                  rows={22}
                  spellCheck={false}
                  className="min-h-[440px] w-full rounded-xl border border-border bg-background px-4 py-3 font-mono text-sm focus:border-primary focus:outline-none"
                />

                <div className="flex flex-wrap items-center gap-3">
                  <button
                    type="button"
                    onClick={() => handlePromptRestoreDefault(activePrompt)}
                    className={cn(
                      "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-4 text-sm font-medium transition-colors",
                      "bg-muted hover:bg-muted/80",
                    )}
                  >
                    <span>{t("ai_settings.prompt_templates_restore_default")}</span>
                  </button>

                  <button
                    type="button"
                    onClick={handlePromptSave}
                    disabled={isPromptSaving || !promptHasChanges}
                    className={cn(
                      "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-4 text-sm font-medium transition-colors",
                      "bg-primary text-primary-foreground hover:bg-primary/90",
                      "disabled:cursor-not-allowed disabled:opacity-50",
                    )}
                  >
                    {isPromptSaving ? (
                      <>
                        <div className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                        <span>{t("ai_settings.prompt_templates_saving")}</span>
                      </>
                    ) : (
                      <span>{t("ai_settings.prompt_templates_save")}</span>
                    )}
                  </button>

                  {promptSuccessMessage && <div className="text-sm text-green-600">{promptSuccessMessage}</div>}
                </div>

                <div className="text-xs text-muted-foreground">
                  {t("ai_settings.prompt_templates_new_requests_hint")}
                </div>
              </div>
            )}
          </div>
        )}
      </section>

      <section className="space-y-4 rounded-xl border border-border/70 bg-card/30 p-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="pb-1">
            <h3 className="text-sm font-semibold text-foreground">{t("ai_settings.usage_title")}</h3>
            <p className="mt-1 text-xs text-muted-foreground">{t("ai_settings.usage_hint")}</p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">{t("ai_settings.usage_query_days_label")}</span>
            <select
              value={usageDays}
              onChange={(e) => setUsageDays(parseInt(e.target.value, 10) || 30)}
              className={cn(selectClass, "h-8 w-28 shrink-0 text-xs")}
            >
              {[7, 30, 90, 365].map((days) => (
                <option key={days} value={days}>
                  {t("ai_settings.usage_recent_days", { count: days })}
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => void loadUsageStats(usageDays)}
              disabled={isUsageLoading}
              className={cn(
                "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors",
                "bg-muted hover:bg-muted/80",
                "disabled:cursor-not-allowed disabled:opacity-50",
              )}
            >
              {isUsageLoading ? (
                <>
                  <div className="size-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
                  <span>{t("ai_settings.usage_querying")}</span>
                </>
              ) : (
                <span>{t("ai_settings.usage_query_button")}</span>
              )}
            </button>
          </div>
        </div>

        {usageError ? (
          <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {usageError}
          </div>
        ) : !usageStats ? (
          <div className="text-sm text-muted-foreground">{t("ai_settings.usage_empty")}</div>
        ) : (
          <>
            <div className="grid gap-3 md:grid-cols-3">
              {[
                { key: "today", title: t("ai_settings.usage_today"), stats: usageStats.today },
                { key: "last7", title: t("ai_settings.usage_last7days"), stats: usageStats.last7Days },
                { key: "all", title: t("ai_settings.usage_all_time"), stats: usageStats.allTime },
              ].map((item) => (
                <div key={item.key} className="rounded-xl border border-border/70 bg-background/70 p-4">
                  <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {item.title}
                  </div>
                  <div className="mt-2 text-2xl font-semibold text-foreground">
                    {formatUsageNumber(item.stats.totalTokens)}
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {t("ai_settings.usage_total_tokens")}
                  </div>
                  <div className="mt-3 flex flex-wrap gap-3 text-xs text-muted-foreground">
                    <span>{t("ai_settings.usage_requests", { count: item.stats.requestCount })}</span>
                    <span>{t("ai_settings.usage_prompt_tokens", { count: item.stats.promptTokens })}</span>
                    <span>{t("ai_settings.usage_completion_tokens", { count: item.stats.completionTokens })}</span>
                  </div>
                </div>
              ))}
            </div>

            <div className="space-y-3">
              <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {t("ai_settings.usage_today_breakdown")}
              </div>
              <div className="grid gap-3 md:grid-cols-3">
                {sceneStats.map((item) => (
                  <div key={item.scene} className="rounded-xl border border-border/70 bg-background/70 p-4">
                    <div className="text-sm font-medium text-foreground">
                      {getUsageSceneLabel(item.scene, t)}
                    </div>
                    <div className="mt-2 text-xl font-semibold text-foreground">
                      {formatUsageNumber(item.totalTokens)}
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {t("ai_settings.usage_total_tokens")}
                    </div>
                    <div className="mt-3 space-y-1 text-xs text-muted-foreground">
                      <div>{t("ai_settings.usage_requests", { count: item.requestCount })}</div>
                      <div>{t("ai_settings.usage_prompt_tokens", { count: item.promptTokens })}</div>
                      <div>{t("ai_settings.usage_completion_tokens", { count: item.completionTokens })}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  {t("ai_settings.usage_daily_history")}
                </div>
                <div className="text-xs text-muted-foreground">
                  {t("ai_settings.usage_recent_days", { count: usageDays })}
                </div>
              </div>

              {usageStats.daily.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border/70 bg-background/40 px-4 py-5 text-sm text-muted-foreground">
                  {t("ai_settings.usage_daily_empty")}
                </div>
              ) : (
                <div className="space-y-3">
                  {usageStats.daily.map((day) => (
                    <div key={day.date} className="rounded-xl border border-border/70 bg-background/70 p-4">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="text-sm font-semibold text-foreground">{day.date}</div>
                          <div className="mt-1 flex flex-wrap gap-3 text-xs text-muted-foreground">
                            <span>{t("ai_settings.usage_requests", { count: day.requestCount })}</span>
                            <span>{t("ai_settings.usage_prompt_tokens", { count: day.promptTokens })}</span>
                            <span>{t("ai_settings.usage_completion_tokens", { count: day.completionTokens })}</span>
                          </div>
                        </div>

                        <div className="text-right">
                          <div className="text-xl font-semibold text-foreground">
                            {formatUsageNumber(day.totalTokens)}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {t("ai_settings.usage_total_tokens")}
                          </div>
                        </div>
                      </div>

                      {day.scenes.length > 0 && (
                        <div className="mt-3 flex flex-wrap gap-2">
                          {day.scenes.map((scene) => (
                            <div
                              key={`${day.date}-${scene.scene}`}
                              className="rounded-full border border-border/70 bg-card/60 px-3 py-1 text-xs text-muted-foreground"
                            >
                              {getUsageSceneLabel(scene.scene, t)} · {formatUsageNumber(scene.totalTokens)}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </section>

      <div className="flex flex-wrap items-center gap-3 pt-1">
        <button
          type="button"
          onClick={handleSave}
          disabled={isSaving}
          className={cn(
            "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-4 text-sm font-medium transition-colors",
            "bg-primary text-primary-foreground hover:bg-primary/90",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          {isSaving ? (
            <>
              <div className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
              <span>{t("ai_settings.saving")}</span>
            </>
          ) : (
            <span>{t("ai_settings.save")}</span>
          )}
        </button>

        {successMessage && <div className="text-sm text-green-600">{successMessage}</div>}
        {error && <div className="text-sm text-destructive">{error}</div>}
      </div>
    </div>
  );
}
