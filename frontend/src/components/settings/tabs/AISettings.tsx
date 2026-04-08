import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { getAISettings, updateAISettings, testAIConnection, ApiError } from "@/api";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import type {
  AIModelSettings,
  AIProvider,
  AISettings as AISettingsType,
  OpenAIEndpoint,
  ReasoningEffort,
} from "@/types/settings";

type ModelSectionKey = "analysis" | "translation" | "report";

interface ModelSectionMeta {
  key: ModelSectionKey;
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

  const [settings, setSettings] = useState<AISettingsType | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [testingSection, setTestingSection] = useState<ModelSectionKey | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [testTarget, setTestTarget] = useState<ModelSectionKey | null>(null);
  const [testResult, setTestResult] = useState<{ success: boolean; message?: string; error?: string } | null>(null);

  useEffect(() => {
    void loadSettings();
  }, []);

  const loadSettings = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getAISettings();
      setSettings(data);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("ai_settings.failed_to_load"));
      }
    } finally {
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
