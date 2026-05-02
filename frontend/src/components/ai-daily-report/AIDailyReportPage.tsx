import { useCallback, useState } from "react";
import { useLocation } from "wouter";
import { useTranslation } from "react-i18next";
import { MenuIcon } from "@/components/ui/icons";
import { useAIDailyReport } from "@/hooks/useAIDailyReport";
import type { AIDailyReportCountItem, StoredAIAnalysis } from "@/api";

interface AIDailyReportPageProps {
  isMobile?: boolean;
  onMenuClick?: () => void;
}

function todayLocalDate() {
  const now = new Date();
  const year = now.getFullYear();
  const month = `${now.getMonth() + 1}`.padStart(2, "0");
  const day = `${now.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
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

function MetricPill({ item }: { item: AIDailyReportCountItem }) {
  return (
    <div className="flex items-center justify-between rounded-xl border border-border/70 bg-card px-3 py-2 text-sm">
      <span className="truncate text-foreground/90">{item.name}</span>
      <span className="ml-3 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
        {item.count}
      </span>
    </div>
  );
}

function InsightSection({
  title,
  content,
}: {
  title: string;
  content?: string;
}) {
  if (!content) return null;

  return (
    <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
      <h2 className="text-base font-semibold text-foreground">{title}</h2>
      <div className="mt-3 space-y-2 text-sm leading-7 text-foreground/85">
        {content
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean)
          .map((line, index) => (
            <p key={`${title}-${index}`}>{line}</p>
          ))}
      </div>
    </section>
  );
}

export function AIDailyReportPage({
  isMobile = false,
  onMenuClick,
}: AIDailyReportPageProps) {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const [selectedDate, setSelectedDate] = useState(todayLocalDate);
  const { data, isLoading, error } = useAIDailyReport(selectedDate);

  const openEntry = useCallback(
    (item: StoredAIAnalysis) => {
      navigate(buildEntryPath(item));
    },
    [navigate],
  );

  const topAnalyses = data?.topAnalyses ?? [];
  const topTags = data?.topTags ?? [];
  const topEntities = data?.topEntities ?? [];
  const topFeeds = data?.topFeeds ?? [];
  const focusedTags = data?.focusedTags ?? [];
  const focusedItems = data?.focusedItems ?? [];

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <div className="border-b border-border/60 px-4 py-3 sm:px-6">
        <div className="flex items-start gap-3">
          {isMobile && (
            <button
              type="button"
              onClick={onMenuClick}
              className="inline-flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-background text-foreground"
              aria-label={t("actions.show_sidebar")}
            >
              <MenuIcon className="size-5" />
            </button>
          )}
          <div className="min-w-0 flex-1">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <div className="min-w-0">
                <h1 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
                  {t("ai_daily_report_page.title")}
                </h1>
                <p className="text-sm text-muted-foreground">
                  {t("ai_daily_report_page.description")}
                </p>
              </div>

              <label className="flex flex-col gap-1 text-sm text-muted-foreground">
                <span>{t("ai_daily_report_page.date_label")}</span>
                <input
                  type="date"
                  value={selectedDate}
                  onChange={(event) => setSelectedDate(event.target.value)}
                  className="h-10 rounded-lg border border-border bg-background px-3 text-sm text-foreground outline-none transition-colors focus:border-primary"
                />
              </label>
            </div>
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
              : t("ai_daily_report_page.load_failed")}
          </div>
        )}

        {!isLoading && !error && data && (
          <div className="space-y-6">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-7">
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("ai_daily_report_page.summary_date")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-foreground">
                  {data.date}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("ai_daily_report_page.total")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-foreground">
                  {data.total}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("ai_daily_report_page.pending_count")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-amber-600 dark:text-amber-300">
                  {data.pendingCount}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {data.pendingCount > 0
                    ? t("ai_daily_report_page.pending_hint_processing")
                    : t("ai_daily_report_page.pending_hint_idle")}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("ai_daily_report_page.focused_total")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-rose-600 dark:text-rose-300">
                  {data.focusedTotal}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t("ai_daily_report_page.focused_hint")}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("entry.analysis_sentiment_positive")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-lime-600 dark:text-lime-300">
                  {data.sentiment.positive}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("entry.analysis_sentiment_neutral")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-sky-600 dark:text-sky-300">
                  {data.sentiment.neutral}
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card p-4">
                <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                  {t("entry.analysis_sentiment_negative")}
                </p>
                <p className="mt-2 text-2xl font-semibold text-rose-600 dark:text-rose-300">
                  {data.sentiment.negative}
                </p>
              </div>
            </div>

            {data.total === 0 && (
              <div className="rounded-xl border border-dashed border-border bg-card px-4 py-8 text-center text-sm text-muted-foreground">
                {t("ai_daily_report_page.empty")}
              </div>
            )}

            {data.total > 0 && (
              <div className="space-y-6">
                {(data.overview || data.riskReview || data.trendOutlook) && (
                  <div className="grid gap-4 xl:grid-cols-3">
                    <InsightSection
                      title={t("ai_daily_report_page.overview")}
                      content={data.overview}
                    />
                    <InsightSection
                      title={t("ai_daily_report_page.risk_review")}
                      content={data.riskReview}
                    />
                    <InsightSection
                      title={t("ai_daily_report_page.trend_outlook")}
                      content={data.trendOutlook}
                    />
                  </div>
                )}

                <div className="grid gap-6 xl:grid-cols-[minmax(0,1.5fr)_minmax(300px,1fr)]">
                <section className="space-y-4">
                  <div className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <div className="flex items-center justify-between gap-3">
                      <h2 className="text-base font-semibold text-foreground sm:text-lg">
                        {t("ai_daily_report_page.top_articles")}
                      </h2>
                      <span className="text-xs text-muted-foreground">
                        {t("ai_daily_report_page.sorted_by_importance")}
                      </span>
                    </div>

                    <div className="mt-4 space-y-3">
                      {topAnalyses.map((item) => {
                        const createdAt = formatDateTime(item.createdAt);
                        const publishedAt = formatDateTime(item.publishedAt);
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
                            className="w-full rounded-2xl border border-border/60 bg-background p-4 text-left transition-colors hover:bg-accent/20"
                          >
                            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                              <span className="rounded-full bg-primary/10 px-2.5 py-1 text-primary">
                                {item.feedTitle}
                              </span>
                              <span className="rounded-full bg-sky-500/10 px-2.5 py-1 text-sky-700 dark:text-sky-300">
                                {t("entry.analysis_importance")}:{" "}
                                {item.importance}
                              </span>
                              {item.focused && (
                                <span className="rounded-full bg-rose-500/10 px-2.5 py-1 text-rose-700 dark:text-rose-300">
                                  {t("ai_daily_report_page.focused_badge")}
                                </span>
                              )}
                              {createdAt && (
                                <span>
                                  {t("ai_analysis_page.generated_at")}:{" "}
                                  {createdAt}
                                </span>
                              )}
                            </div>

                            <div className="mt-3 space-y-3">
                              <div>
                                <h3 className="line-clamp-2 text-base font-semibold text-foreground">
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
                                {(item.focusTags ?? []).map((tag) => (
                                  <span
                                    key={`${item.id}-${tag}`}
                                    className="rounded-full bg-rose-500/10 px-2.5 py-1 text-rose-700 dark:text-rose-300"
                                  >
                                    #{tag}
                                  </span>
                                ))}
                                <span className="rounded-full bg-muted px-2.5 py-1 text-muted-foreground">
                                  {t("entry.analysis_sentiment")}:{" "}
                                  {sentimentKey
                                    ? t(sentimentKey)
                                    : item.sentiment}
                                </span>
                              </div>

                              <p className="line-clamp-3 text-sm leading-7 text-foreground/85">
                                {item.summary}
                              </p>
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                </section>

                <aside className="space-y-4">
                  <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <h2 className="text-base font-semibold text-foreground">
                      {t("ai_daily_report_page.focused_items")}
                    </h2>
                    <div className="mt-4 space-y-3">
                      {focusedItems.length > 0 ? (
                        focusedItems.map((item) => (
                          <button
                            key={`focused-${item.id}`}
                            type="button"
                            onClick={() => openEntry(item)}
                            className="w-full rounded-xl border border-border/70 bg-background px-3 py-3 text-left transition-colors hover:bg-accent/20"
                          >
                            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                              <span className="rounded-full bg-rose-500/10 px-2 py-1 text-rose-700 dark:text-rose-300">
                                {item.feedTitle}
                              </span>
                              <span>{t("entry.analysis_importance")}: {item.importance}</span>
                            </div>
                            <div className="mt-2 line-clamp-2 text-sm font-medium text-foreground">
                              {item.entryTitle || t("entry.untitled")}
                            </div>
                            {(item.focusTags ?? []).length > 0 && (
                              <div className="mt-2 flex flex-wrap gap-2 text-xs">
                                {(item.focusTags ?? []).map((tag) => (
                                  <span
                                    key={`focused-${item.id}-${tag}`}
                                    className="rounded-full bg-primary/10 px-2 py-1 text-primary"
                                  >
                                    #{tag}
                                  </span>
                                ))}
                              </div>
                            )}
                          </button>
                        ))
                      ) : (
                        <p className="text-sm text-muted-foreground">
                          {t("entry.analysis_none")}
                        </p>
                      )}
                    </div>
                  </section>

                  <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <h2 className="text-base font-semibold text-foreground">
                      {t("ai_daily_report_page.focused_tags")}
                    </h2>
                    <div className="mt-4 space-y-2">
                      {focusedTags.length > 0 ? (
                        focusedTags.map((item) => (
                          <MetricPill key={`focused-tag-${item.name}`} item={item} />
                        ))
                      ) : (
                        <p className="text-sm text-muted-foreground">
                          {t("entry.analysis_none")}
                        </p>
                      )}
                    </div>
                  </section>

                  <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <h2 className="text-base font-semibold text-foreground">
                      {t("ai_daily_report_page.top_tags")}
                    </h2>
                    <div className="mt-4 space-y-2">
                      {topTags.length > 0 ? (
                        topTags.map((item) => (
                          <MetricPill key={`tag-${item.name}`} item={item} />
                        ))
                      ) : (
                        <p className="text-sm text-muted-foreground">
                          {t("entry.analysis_none")}
                        </p>
                      )}
                    </div>
                  </section>

                  <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <h2 className="text-base font-semibold text-foreground">
                      {t("ai_daily_report_page.top_entities")}
                    </h2>
                    <div className="mt-4 space-y-2">
                      {topEntities.length > 0 ? (
                        topEntities.map((item) => (
                          <MetricPill key={`entity-${item.name}`} item={item} />
                        ))
                      ) : (
                        <p className="text-sm text-muted-foreground">
                          {t("entry.analysis_none")}
                        </p>
                      )}
                    </div>
                  </section>

                  <section className="rounded-2xl border border-border/70 bg-card p-4 sm:p-5">
                    <h2 className="text-base font-semibold text-foreground">
                      {t("ai_daily_report_page.top_feeds")}
                    </h2>
                    <div className="mt-4 space-y-2">
                      {topFeeds.length > 0 ? (
                        topFeeds.map((item) => (
                          <div
                            key={`feed-${item.feedId}`}
                            className="flex items-center justify-between rounded-xl border border-border/70 bg-background px-3 py-2 text-sm"
                          >
                            <span className="truncate text-foreground/90">
                              {item.feedTitle}
                            </span>
                            <span className="ml-3 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                              {item.count}
                            </span>
                          </div>
                        ))
                      ) : (
                        <p className="text-sm text-muted-foreground">
                          {t("entry.analysis_none")}
                        </p>
                      )}
                    </div>
                  </section>
                </aside>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
