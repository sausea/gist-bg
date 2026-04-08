import { useEffect, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { exportEntryMarkdown } from "@/api";
import {
  useEntry,
  useMarkAsRead,
  useMarkAsStarred,
  useRemoveFromUnreadList,
} from "@/hooks/useEntries";
import { useAISettings } from "@/hooks/useAISettings";
import { useGeneralSettings } from "@/hooks/useGeneralSettings";
import { useEntryContentScroll } from "@/hooks/useEntryContentScroll";
import { useScrollToTop } from "@/hooks/useScrollToTop";
import { useReadability } from "@/hooks/useReadability";
import { useAIAnalysis } from "@/hooks/useAIAnalysis";
import { useAITranslation } from "@/hooks/useAITranslation";
import { useAIProcessingStatus } from "@/hooks/useAIProcessingStatus";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { EntryContentHeader } from "./EntryContentHeader";
import { EntryContentBody } from "./EntryContentBody";

interface EntryContentProps {
  entryId: string | null;
  isMobile?: boolean;
  onBack?: () => void;
}

export function EntryContent({ entryId, isMobile, onBack }: EntryContentProps) {
  const { t } = useTranslation();
  const { data: entry, isLoading } = useEntry(entryId);
  const { data: aiSettings } = useAISettings();
  const { data: generalSettings } = useGeneralSettings();
  const { mutate: markAsRead } = useMarkAsRead();
  const { mutate: markAsStarred } = useMarkAsStarred();
  const removeFromUnreadList = useRemoveFromUnreadList();
  const { scrollRef, isAtTop, scrollNode } = useEntryContentScroll(entryId);

  useScrollToTop(scrollNode, "entrycontent");

  // Track entries marked as read to trigger list removal on switch
  const markedAsReadRef = useRef<Set<string>>(new Set());

  const autoTranslate = aiSettings?.autoTranslate ?? false;
  const autoTranslateTitle = aiSettings?.autoTranslateTitle ?? false;
  const targetLanguage = aiSettings?.summaryLanguage ?? "zh-CN";
  const autoReadability = generalSettings?.autoReadability ?? false;
  const autoAnalysis = aiSettings?.autoAnalysis ?? false;

  const [exportOpen, setExportOpen] = useState(false);
  const [exportTags, setExportTags] = useState("");
  const [exportError, setExportError] = useState<string | null>(null);
  const [isExporting, setIsExporting] = useState(false);

  useEffect(() => {
    setExportTags("");
    setExportError(null);
  }, [entry?.id]);

  // Readability hook
  const {
    isReadableLoading,
    readableContent,
    readableError,
    isReadableActive,
    baseContent,
    handleToggleReadable,
  } = useReadability({ entry, autoReadability });

  const [showAnalysis, setShowAnalysis] = useState(autoAnalysis);

  const { aiAnalysis, isLoadingAnalysis, analysisError, requestAnalysis, cancelAnalysis } =
    useAIAnalysis({
      entry,
      isReadableActive,
      readableContent,
      autoAnalysis,
    });

  useEffect(() => {
    setShowAnalysis(autoAnalysis);
  }, [entry?.id, autoAnalysis]);

  const { data: aiProcessingStatus } = useAIProcessingStatus({
    entryId,
    enabled: true,
  });

  const isBackgroundAIProcessing = !!aiProcessingStatus?.processing;
  const isBackgroundAnalysisProcessing =
    showAnalysis &&
    isBackgroundAIProcessing &&
    !aiAnalysis &&
    !isLoadingAnalysis &&
    !analysisError;

  // AI Translation hook
  const {
    isTranslating,
    hasTranslation,
    translationDisabled,
    displayTitle,
    translatedTitle,
    translatedContentBlocks,
    handleToggleTranslation,
  } = useAITranslation({
    entry,
    isReadableActive,
    readableContent,
    autoTranslate,
    autoTranslateTitle,
    targetLanguage,
  });

  // Mark as read when entry is loaded
  // Use skipInvalidate to prevent list item from disappearing immediately
  useEffect(() => {
    if (entry && !entry.read) {
      markedAsReadRef.current.add(entry.id);
      markAsRead({ id: entry.id, read: true, skipInvalidate: true });
    }
  }, [entry, markAsRead]);

  // Remove read entries from unreadOnly list when component unmounts (switching articles)
  // Note: EntryContent uses key={entryId} in App.tsx, so it unmounts/remounts on switch
  useEffect(() => {
    const markedAsReadSet = markedAsReadRef.current;
    return () => {
      if (markedAsReadSet.size > 0) {
        removeFromUnreadList(markedAsReadSet);
        markedAsReadSet.clear();
      }
    };
  }, [removeFromUnreadList]);

  const handleOpenExport = useCallback(() => {
    if (!entry) return;
    setExportError(null);
    setExportOpen(true);
  }, [entry]);

  const handleExportClose = useCallback(() => {
    setExportOpen(false);
    setExportError(null);
  }, []);

  const handleExportOpenChange = useCallback((open: boolean) => {
    setExportOpen(open);
    if (!open) {
      setExportError(null);
    }
  }, []);

  const handleExportSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!entry || isExporting) return;

      setIsExporting(true);
      setExportError(null);
      const tags = exportTags
        .split(",")
        .map((tag) => tag.trim())
        .filter(Boolean);

      try {
        await exportEntryMarkdown(entry.id, tags);
        if (!entry.starred) {
          markAsStarred({ id: entry.id, starred: true });
        }
        setExportTags("");
        setExportOpen(false);
      } catch {
        setExportError(t("entry.export_failed"));
      } finally {
        setIsExporting(false);
      }
    },
    [entry, exportTags, isExporting, markAsStarred, t],
  );

  const handleUnstar = useCallback(() => {
    if (!entry) return;
    markAsStarred({ id: entry.id, starred: false });
    setExportOpen(false);
  }, [entry, markAsStarred]);

  const handleToggleAnalysis = useCallback(async () => {
    if (!entry) return;

    if (showAnalysis) {
      if (isLoadingAnalysis && !aiAnalysis) {
        cancelAnalysis();
      }
      setShowAnalysis(false);
      return;
    }

    setShowAnalysis(true);
    if (!aiAnalysis && !isLoadingAnalysis) {
      await requestAnalysis(isReadableActive);
    }
  }, [
    entry,
    showAnalysis,
    isLoadingAnalysis,
    aiAnalysis,
    cancelAnalysis,
    requestAnalysis,
    isReadableActive,
  ]);

  // Determine display content:
  // - Always keep original content as the baseline.
  // - When translation is enabled, render bilingual blocks (original + translation under each block).
  const displayContent = baseContent;
  const highlightContent = translatedContentBlocks
    ? translatedContentBlocks.map((b) => b.html).join("")
    : (baseContent ?? "");

  if (entryId === null) {
    return <EntryContentEmpty message={t("entry.select_article")} />;
  }

  if (isLoading) {
    return <EntryContentSkeleton />;
  }

  if (!entry) {
    return <EntryContentEmpty message={t("entry.select_article")} />;
  }

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden">
      <EntryContentHeader
        entry={entry}
        displayTitle={displayTitle}
        isAtTop={isAtTop}
        isReadableActive={isReadableActive}
        isLoading={isReadableLoading}
        error={readableError}
        onToggleReadable={handleToggleReadable}
        onOpenStarDialog={handleOpenExport}
        isLoadingAnalysis={showAnalysis && isLoadingAnalysis}
        hasAnalysis={showAnalysis}
        onToggleAnalysis={handleToggleAnalysis}
        isTranslating={isTranslating}
        hasTranslation={hasTranslation}
        translationDisabled={translationDisabled}
        onToggleTranslation={handleToggleTranslation}
        isMobile={isMobile}
        onBack={onBack}
      />
      <Dialog open={exportOpen} onOpenChange={handleExportOpenChange}>
        <DialogContent className="max-w-md p-0">
          <DialogHeader className="border-b border-border px-4 py-3">
            <DialogTitle>{t("entry.export_title")}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleExportSubmit} className="space-y-4 p-4">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">
                {t("entry.export_tags_label")}
              </label>
              <input
                type="text"
                value={exportTags}
                onChange={(e) => setExportTags(e.target.value)}
                placeholder={t("entry.export_tags_placeholder")}
                className={cn(
                  "w-full rounded-md border border-border bg-background px-3 py-2 text-sm",
                  "focus:outline-none focus:ring-2 focus:ring-primary/50",
                  "placeholder:text-muted-foreground",
                )}
                autoFocus
              />
            </div>
            {exportError && (
              <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {exportError}
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              {entry.starred && (
                <button
                  type="button"
                  onClick={handleUnstar}
                  className={cn(
                    "rounded-md px-4 py-2 text-sm font-medium transition-colors",
                    "border border-destructive/40 text-destructive hover:bg-destructive/10",
                  )}
                >
                  {t("entry.remove_from_starred")}
                </button>
              )}
              <button
                type="button"
                onClick={handleExportClose}
                className={cn(
                  "rounded-md px-4 py-2 text-sm font-medium transition-colors",
                  "border border-border bg-background hover:bg-muted",
                )}
              >
                {t("actions.cancel")}
              </button>
              <button
                type="submit"
                disabled={isExporting}
                className={cn(
                  "rounded-md px-4 py-2 text-sm font-medium transition-colors",
                  "bg-primary text-primary-foreground hover:bg-primary/90",
                  "disabled:cursor-not-allowed disabled:opacity-50",
                )}
              >
                {isExporting ? t("entry.exporting") : t("entry.export_save")}
              </button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <EntryContentBody
        entry={entry}
        displayTitle={displayTitle}
        translatedTitle={translatedTitle}
        scrollRef={scrollRef}
        scrollNode={scrollNode}
        displayContent={displayContent}
        displayBlocks={translatedContentBlocks}
        highlightContent={highlightContent}
        aiAnalysis={showAnalysis ? aiAnalysis : null}
        isLoadingAnalysis={showAnalysis ? isLoadingAnalysis : false}
        analysisError={showAnalysis ? analysisError : null}
        isBackgroundAIProcessing={isBackgroundAIProcessing}
        aiProcessingQueued={!!aiProcessingStatus?.queued}
        isBackgroundAnalysisProcessing={isBackgroundAnalysisProcessing}
      />
    </div>
  );
}

function EntryContentEmpty({ message }: { message: string }) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 items-center px-6" />
      <div className="flex flex-1 items-center justify-center">
        <div className="text-center text-muted-foreground">
          <svg
            className="mx-auto size-12 opacity-50"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1.5}
              d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
            />
          </svg>
          <p className="mt-2 text-sm">{message}</p>
        </div>
      </div>
    </div>
  );
}

function EntryContentSkeleton() {
  return (
    <div className="relative flex h-full flex-col animate-pulse">
      <div className="absolute inset-x-0 top-0 z-20">
        <div className="h-12" />
      </div>
      <div className="flex-1 overflow-auto">
        <div className="mx-auto w-full max-w-[720px] px-6 pb-20 pt-16">
          <div className="mb-10 space-y-5">
            <div className="h-10 w-3/4 rounded bg-muted" />
            <div className="flex gap-6">
              <div className="h-4 w-24 rounded bg-muted" />
              <div className="h-4 w-32 rounded bg-muted" />
            </div>
            <hr className="border-border/60" />
          </div>
          <div className="space-y-4">
            <div className="h-4 w-full rounded bg-muted" />
            <div className="h-4 w-full rounded bg-muted" />
            <div className="h-4 w-3/4 rounded bg-muted" />
            <div className="h-4 w-full rounded bg-muted" />
            <div className="h-4 w-5/6 rounded bg-muted" />
          </div>
        </div>
      </div>
    </div>
  );
}
