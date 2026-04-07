import { useCallback } from "react";
import { useLocation } from "wouter";
import { useTranslation } from "react-i18next";
import { useStoredAIAnalyses } from "@/hooks/useStoredAIAnalyses";
import { MenuIcon } from "@/components/ui/icons";
import type { StoredAIAnalysis } from "@/api";

interface AIAnalysesPageProps {
  isMobile?: boolean;
  onMenuClick?: () => void;
}

function formatDateTime(value?: string) {
  if (!value) return null;

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;

  return date.toLocaleString();
}

function buildEntryPath(item: StoredAIAnalysis) {
  return `/feed/${item.feedId}/${item.entryId}?type=${item.feedType}`;
}

export function AIAnalysesPage({
  isMobile = false,
  onMenuClick,
}: AIAnalysesPageProps) {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const { data, isLoading, error } = useStoredAIAnalyses({ limit: 100 });

  const openEntry = useCallback(
    (item: StoredAIAnalysis) => {
      if (item.entryUrl) {
        window.open(item.entryUrl, "_blank", "noopener,noreferrer");
        return;
      }
      navigate(buildEntryPath(item));
    },
    [navigate],
  );

  const items = data?.items ?? [];

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <div className="border-b border-border/60 px-4 py-3 sm:px-6">
        <div className="flex items-center gap-3">
          {isMobile && (
            <button
              type="button"
              onClick={onMenuClick}
              className="inline-flex size-9 items-center justify-center rounded-md border border-border bg-background text-foreground"
              aria-label={t("actions.show_sidebar")}
            >
              <MenuIcon className="size-5" />
            </button>
          )}
          <div className="min-w-0">
            <h1 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
              {t("ai_analysis_page.title")}
            </h1>
            <p className="text-sm text-muted-foreground">
              {t("ai_analysis_page.description")}
            </p>
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-6">
        {isLoading && (
          <div className="rounded-xl border border-border bg-card px-4 py-8 text-center text-sm text-muted-foreground">
            {t("entry.loading")}
          </div>
        )}

        {!isLoading && error && (
          <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-8 text-center text-sm text-destructive">
            {error instanceof Error
              ? error.message
              : t("ai_analysis_page.load_failed")}
          </div>
        )}

        {!isLoading && !error && items.length === 0 && (
          <div className="rounded-xl border border-dashed border-border bg-card px-4 py-8 text-center text-sm text-muted-foreground">
            {t("ai_analysis_page.empty")}
          </div>
        )}

        {!isLoading && !error && items.length > 0 && (
          <div className="space-y-4">
            {items.map((item) => {
              const publishedAt = formatDateTime(item.publishedAt);
              const createdAt = formatDateTime(item.createdAt);
              const sentimentKey =
                item.sentiment === "positive"
                  ? "entry.analysis_sentiment_positive"
                  : item.sentiment === "negative"
                    ? "entry.analysis_sentiment_negative"
                    : item.sentiment === "neutral"
                      ? "entry.analysis_sentiment_neutral"
                      : null;

              return (
                <button
                  key={`${item.id}-${item.language}`}
                  type="button"
                  onClick={() => openEntry(item)}
                  className="w-full rounded-2xl border border-border/70 bg-card p-4 text-left transition-colors hover:bg-accent/20 sm:p-5"
                >
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span className="rounded-full bg-primary/10 px-2.5 py-1 text-primary">
                      {item.feedTitle}
                    </span>
                    <span className="rounded-full bg-muted px-2.5 py-1">
                      {item.language}
                    </span>
                    {item.isReadability && (
                      <span className="rounded-full bg-amber-500/10 px-2.5 py-1 text-amber-700 dark:text-amber-300">
                        {t("ai_analysis_page.readability")}
                      </span>
                    )}
                    {createdAt && (
                      <span>
                        {t("ai_analysis_page.generated_at")}: {createdAt}
                      </span>
                    )}
                  </div>

                  <div className="mt-3 space-y-3">
                    <div>
                      <h2 className="line-clamp-2 text-base font-semibold text-foreground sm:text-lg">
                        {item.entryTitle || t("entry.untitled")}
                      </h2>
                      <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
                        {item.author && <span>{item.author}</span>}
                        {publishedAt && <span>{publishedAt}</span>}
                      </div>
                    </div>

                    <div className="flex flex-wrap gap-2 text-xs">
                      <span className="rounded-full bg-lime-500/10 px-2.5 py-1 text-lime-700 dark:text-lime-300">
                        {item.tag}
                      </span>
                      <span className="rounded-full bg-sky-500/10 px-2.5 py-1 text-sky-700 dark:text-sky-300">
                        {t("entry.analysis_importance")}: {item.importance}
                      </span>
                      <span className="rounded-full bg-muted px-2.5 py-1 text-muted-foreground">
                        {t("entry.analysis_sentiment")}:{" "}
                        {sentimentKey ? t(sentimentKey) : item.sentiment}
                      </span>
                    </div>

                    <p className="line-clamp-4 text-sm leading-7 text-foreground/85">
                      {item.summary}
                    </p>

                    {item.entities.length > 0 && (
                      <div className="flex flex-wrap gap-2">
                        {item.entities.map((entity) => (
                          <span
                            key={`${item.id}-${entity}`}
                            className="rounded-full border border-border/80 px-2.5 py-1 text-xs text-muted-foreground"
                          >
                            {entity}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
