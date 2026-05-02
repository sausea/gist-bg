import { useCallback } from "react";
import { useLocation } from "wouter";
import { useTranslation } from "react-i18next";
import { useStoredAIAnalyses } from "@/hooks/useStoredAIAnalyses";
import { useAIAnalysisQueue } from "@/hooks/useAIAnalysisQueue";
import { MenuIcon } from "@/components/ui/icons";
import type { AIAnalysisQueueItem, StoredAIAnalysis } from "@/api";

interface AIAnalysesPageProps {
  isMobile?: boolean;
  onMenuClick?: () => void;
}

type NavigableAIItem = Pick<
  StoredAIAnalysis | AIAnalysisQueueItem,
  "feedId" | "entryId" | "feedType" | "entryUrl"
>;

function formatDateTime(value?: string) {
  if (!value) return null;

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;

  return date.toLocaleString();
}

function buildEntryPath(item: NavigableAIItem) {
  return `/feed/${item.feedId}/${item.entryId}?type=${item.feedType}`;
}

function queueStatusClasses(status: string) {
  switch (status) {
    case "running":
      return "bg-sky-500/10 text-sky-700 dark:text-sky-300";
    case "queued":
      return "bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "failed":
      return "bg-rose-500/10 text-rose-700 dark:text-rose-300";
    default:
      return "bg-muted text-muted-foreground";
  }
}

function statValueClasses(tone: "amber" | "sky" | "rose" | "primary") {
  switch (tone) {
    case "amber":
      return "text-amber-600 dark:text-amber-300";
    case "sky":
      return "text-sky-600 dark:text-sky-300";
    case "rose":
      return "text-rose-600 dark:text-rose-300";
    default:
      return "text-foreground";
  }
}

function QueueStatCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: "amber" | "sky" | "rose" | "primary";
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-card p-4">
      <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
        {label}
      </p>
      <p className={`mt-2 text-2xl font-semibold ${statValueClasses(tone)}`}>
        {value}
      </p>
    </div>
  );
}

export function AIAnalysesPage({
  isMobile = false,
  onMenuClick,
}: AIAnalysesPageProps) {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const {
    data: queueData,
    isLoading: isQueueLoading,
    error: queueError,
  } = useAIAnalysisQueue({ limit: 50 });
  const {
    data,
    isLoading,
    error,
  } = useStoredAIAnalyses({
    limit: 10,
    refetchInterval: queueData?.processing ? 5000 : false,
  });

  const openEntry = useCallback(
    (item: NavigableAIItem) => {
      if (item.entryUrl) {
        window.open(item.entryUrl, "_blank", "noopener,noreferrer");
        return;
      }
      navigate(buildEntryPath(item));
    },
    [navigate],
  );

  const items = data?.items ?? [];
  const queueItems = queueData?.items ?? [];
  const queuedPositions: Record<string, number> = {};
  let queuedIndex = 0;
  for (const item of queueItems) {
    if (item.status === "queued") {
      queuedIndex += 1;
      queuedPositions[String(item.id)] = queuedIndex;
    }
  }

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
        <div className="space-y-8">
          <section className="space-y-4">
            <div className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
                <div>
                  <h2 className="text-base font-semibold text-foreground sm:text-lg">
                    {t("ai_analysis_page.queue_section")}
                  </h2>
                  <p className="text-sm text-muted-foreground">
                    {t("ai_analysis_page.queue_description")}
                  </p>
                </div>
                <span className="text-xs text-muted-foreground">
                  {queueData?.processing
                    ? t("ai_analysis_page.queue_refresh_active")
                    : t("ai_analysis_page.queue_refresh_idle")}
                </span>
              </div>

              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <QueueStatCard
                  label={t("ai_analysis_page.queue_pending")}
                  value={queueData?.pendingCount ?? 0}
                  tone="primary"
                />
                <QueueStatCard
                  label={t("ai_analysis_page.queue_running")}
                  value={queueData?.runningCount ?? 0}
                  tone="sky"
                />
                <QueueStatCard
                  label={t("ai_analysis_page.queue_queued")}
                  value={queueData?.queuedCount ?? 0}
                  tone="amber"
                />
                <QueueStatCard
                  label={t("ai_analysis_page.queue_failed")}
                  value={queueData?.failedCount ?? 0}
                  tone="rose"
                />
              </div>
            </div>

            {isQueueLoading && (
              <div className="rounded-xl border border-border bg-card px-4 py-8 text-center text-sm text-muted-foreground">
                {t("entry.loading")}
              </div>
            )}

            {!isQueueLoading && queueError && (
              <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-8 text-center text-sm text-destructive">
                {queueError instanceof Error
                  ? queueError.message
                  : t("ai_analysis_page.queue_load_failed")}
              </div>
            )}

            {!isQueueLoading && !queueError && queueItems.length === 0 && (
              <div className="rounded-xl border border-dashed border-border bg-card px-4 py-8 text-center text-sm text-muted-foreground">
                {t("ai_analysis_page.queue_empty")}
              </div>
            )}

            {!isQueueLoading && !queueError && queueItems.length > 0 && (
              <div className="space-y-3">
                {queueItems.map((item) => {
                  const publishedAt = formatDateTime(item.publishedAt);
                  const createdAt = formatDateTime(item.createdAt);
                  const startedAt = formatDateTime(item.startedAt);
                  const updatedAt = formatDateTime(item.updatedAt);
                  const statusLabel =
                    item.status === "running"
                      ? t("ai_analysis_page.queue_running")
                      : item.status === "queued"
                        ? t("ai_analysis_page.queue_queued")
                        : item.status === "failed"
                          ? t("ai_analysis_page.queue_failed")
                          : item.status;
                  const sourceLabel =
                    item.source === "manual"
                      ? t("ai_analysis_page.queue_source_manual")
                      : t("ai_analysis_page.queue_source_auto");
                  const progressText =
                    item.status === "running"
                      ? t("ai_analysis_page.queue_running_hint", {
                          time: startedAt || updatedAt || createdAt || "-",
                        })
                      : item.status === "queued"
                        ? t("ai_analysis_page.queue_waiting_position", {
                            count: queuedPositions[String(item.id)] ?? 1,
                          })
                        : t("ai_analysis_page.queue_failed_hint", {
                            time: updatedAt || createdAt || "-",
                          });

                  return (
                    <button
                      key={`${item.id}-${item.status}-${item.updatedAt}`}
                      type="button"
                      onClick={() => openEntry(item)}
                      className="w-full rounded-2xl border border-border/70 bg-card p-4 text-left transition-colors hover:bg-accent/20 sm:p-5"
                    >
                      <div className="flex flex-col gap-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                            <span className="rounded-full bg-primary/10 px-2.5 py-1 text-primary">
                              {item.feedTitle}
                            </span>
                            <span className="rounded-full bg-muted px-2.5 py-1">
                              {item.language}
                            </span>
                            <span className="rounded-full bg-muted px-2.5 py-1">
                              {sourceLabel}
                            </span>
                            {item.contentMode === "readability" && (
                              <span className="rounded-full bg-amber-500/10 px-2.5 py-1 text-amber-700 dark:text-amber-300">
                                {t("ai_analysis_page.readability")}
                              </span>
                            )}
                            {item.retryCount > 0 && (
                              <span className="rounded-full bg-muted px-2.5 py-1">
                                {t("ai_analysis_page.queue_retry_count", {
                                  count: item.retryCount,
                                })}
                              </span>
                            )}
                          </div>

                          <span
                            className={`rounded-full px-2.5 py-1 text-xs font-medium ${queueStatusClasses(item.status)}`}
                          >
                            {statusLabel}
                          </span>
                        </div>

                        <div>
                          <h3 className="line-clamp-2 text-base font-semibold text-foreground sm:text-lg">
                            {item.entryTitle || t("entry.untitled")}
                          </h3>
                          <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
                            {item.author && <span>{item.author}</span>}
                            {publishedAt && <span>{publishedAt}</span>}
                            {updatedAt && (
                              <span>
                                {t("ai_analysis_page.queue_updated_at")}:{" "}
                                {updatedAt}
                              </span>
                            )}
                          </div>
                        </div>

                        <p className="text-sm text-foreground/80">
                          {progressText}
                        </p>

                        {item.errorMessage && (
                          <div className="rounded-xl border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                            <span className="font-medium">
                              {t("ai_analysis_page.queue_error")}:
                            </span>{" "}
                            {item.errorMessage}
                          </div>
                        )}
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </section>

          <section className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="text-base font-semibold text-foreground sm:text-lg">
                  {t("ai_analysis_page.library_section")}
                </h2>
                <p className="text-sm text-muted-foreground">
                  {t("ai_analysis_page.library_description")}
                </p>
              </div>
              <span className="rounded-full bg-primary/10 px-3 py-1 text-xs font-medium text-primary">
                {items.length}
              </span>
            </div>

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
                          <h3 className="line-clamp-2 text-base font-semibold text-foreground sm:text-lg">
                            {item.entryTitle || t("entry.untitled")}
                          </h3>
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
          </section>
        </div>
      </div>
    </div>
  );
}
