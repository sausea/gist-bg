import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import type { AIAnalysis } from "@/api";

interface AiAnalysisBoxProps {
  analysis: AIAnalysis | null;
  isLoading?: boolean;
  error?: string | null;
  isBackgroundProcessing?: boolean;
}

function getLocalizedSentiment(
  sentiment: AIAnalysis["sentiment"] | undefined,
  t: (key: string) => string,
) {
  switch (sentiment) {
    case "positive":
      return t("entry.analysis_sentiment_positive");
    case "negative":
      return t("entry.analysis_sentiment_negative");
    default:
      return t("entry.analysis_sentiment_neutral");
  }
}

export function AiAnalysisBox({
  analysis,
  isLoading,
  error,
  isBackgroundProcessing,
}: AiAnalysisBoxProps) {
  const { t } = useTranslation();

  if (!analysis && !isLoading && !error && !isBackgroundProcessing) return null;

  const coordinates =
    analysis?.latitude != null && analysis?.longitude != null
      ? `${analysis.latitude.toFixed(6)}, ${analysis.longitude.toFixed(6)}`
      : null;

  return (
    <div
      className={cn(
        "rounded-lg border mb-3 break-words",
        error
          ? "border-destructive/30 bg-destructive/5"
          : "border-emerald-500/20 bg-emerald-500/5",
      )}
    >
      <div className="p-4 sm:p-5">
        <div className="flex items-center gap-2 mb-3">
          <svg
            className={cn(
              "size-4",
              error
                ? "text-destructive"
                : "text-emerald-600 dark:text-emerald-400",
            )}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 17v-2m3 2v-4m3 4V7m4 10H5a2 2 0 01-2-2V5a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2z"
            />
          </svg>
          <h3
            className={cn(
              "text-sm font-semibold",
              error
                ? "text-destructive"
                : "text-emerald-700 dark:text-emerald-300",
            )}
          >
            {t("entry.ai_analysis")}
          </h3>
        </div>

        {error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : isLoading && !analysis ? (
          <div className="space-y-2.5">
            <div className="h-4 w-full rounded ai-shimmer" />
            <div className="h-4 w-4/5 rounded ai-shimmer" />
            <div className="h-4 w-2/3 rounded ai-shimmer" />
          </div>
        ) : isBackgroundProcessing && !analysis ? (
          <p className="text-sm text-muted-foreground">
            {t("entry.ai_processing")}
          </p>
        ) : analysis ? (
          <div className="space-y-3 text-sm leading-relaxed">
            <div>
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {t("entry.analysis_tag")}
              </div>
              <div className="mt-1 font-medium text-foreground">
                {analysis.tag}
              </div>
            </div>
            <div>
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {t("entry.analysis_summary")}
              </div>
              <div className="mt-1 text-foreground">{analysis.summary}</div>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {t("entry.analysis_entities")}
                </div>
                <div className="mt-1 text-foreground">
                  {analysis.entities.length > 0
                    ? analysis.entities.join(", ")
                    : t("entry.analysis_none")}
                </div>
              </div>
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {t("entry.analysis_sentiment")}
                </div>
                <div className="mt-1 text-foreground">
                  {getLocalizedSentiment(analysis.sentiment, t)}
                </div>
              </div>
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {t("entry.analysis_importance")}
                </div>
                <div className="mt-1 text-foreground">
                  {analysis.importance}/10
                </div>
              </div>
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {t("entry.analysis_coordinates")}
                </div>
                <div className="mt-1 text-foreground">
                  {coordinates ?? t("entry.analysis_none")}
                </div>
              </div>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
