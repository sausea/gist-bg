import { useEffect, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useEntry,
  useEntryFocus,
  useMarkAsRead,
  useRemoveFromUnreadList,
  useUpdateEntryFocus,
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
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { EntryContentHeader } from "./EntryContentHeader";
import { EntryContentBody } from "./EntryContentBody";

interface EntryContentProps {
  entryId: string | null;
  isMobile?: boolean;
  onBack?: () => void;
}

function normalizeFocusTags(tags: string[]) {
  return Array.from(
    new Set(
      tags
        .map((tag) => tag.trim().replace(/\s+/g, " "))
        .filter(Boolean),
    ),
  ).sort((left, right) => left.localeCompare(right, "zh-CN"));
}

export function EntryContent({ entryId, isMobile, onBack }: EntryContentProps) {
  const { t } = useTranslation();
  const { data: entry, isLoading } = useEntry(entryId);
  const { data: focusData } = useEntryFocus(entryId);
  const { data: aiSettings } = useAISettings();
  const { data: generalSettings } = useGeneralSettings();
  const { mutate: markAsRead } = useMarkAsRead();
  const { mutateAsync: updateFocus } = useUpdateEntryFocus();
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

  const [focusOpen, setFocusOpen] = useState(false);
  const [focusEnabled, setFocusEnabled] = useState(false);
  const [focusTags, setFocusTags] = useState<string[]>([]);
  const [focusTagInput, setFocusTagInput] = useState("");
  const [focusError, setFocusError] = useState<string | null>(null);
  const [isSavingFocus, setIsSavingFocus] = useState(false);

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

  const {
    data: aiProcessingStatus,
    isFetched: isAIProcessingStatusFetched,
  } = useAIProcessingStatus({
    entryId,
    enabled: true,
  });
  const isBackgroundAIProcessing = !!aiProcessingStatus?.processing;

  const { aiAnalysis, isLoadingAnalysis, analysisError, requestAnalysis, cancelAnalysis } =
    useAIAnalysis({
      entry,
      isReadableActive,
      readableContent,
      autoAnalysis,
      backgroundProcessing: isBackgroundAIProcessing,
      backgroundStatusChecked: isAIProcessingStatusFetched,
    });

  useEffect(() => {
    setShowAnalysis(autoAnalysis);
  }, [entry?.id, autoAnalysis]);

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

  const handleOpenFocus = useCallback(() => {
    if (!entry) return;
    setFocusEnabled(focusData?.focused ?? entry.starred);
    setFocusTags(normalizeFocusTags(focusData?.tags ?? []));
    setFocusTagInput("");
    setFocusError(null);
    setFocusOpen(true);
  }, [entry, focusData?.focused, focusData?.tags, entry?.starred]);

  const handleFocusClose = useCallback(() => {
    setFocusOpen(false);
    setFocusTagInput("");
    setFocusError(null);
  }, []);

  const handleFocusOpenChange = useCallback((open: boolean) => {
    setFocusOpen(open);
    if (!open) {
      setFocusTagInput("");
      setFocusError(null);
    }
  }, []);

  const handleAddFocusTag = useCallback(() => {
    const nextTag = focusTagInput.trim().replace(/\s+/g, " ");
    if (!nextTag) return;
    setFocusTags((current) => normalizeFocusTags([...current, nextTag]));
    setFocusTagInput("");
  }, [focusTagInput]);

  const handleRemoveFocusTag = useCallback((tagToRemove: string) => {
    setFocusTags((current) => current.filter((tag) => tag !== tagToRemove));
  }, []);

  const handleFocusSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!entry || isSavingFocus) return;

      const finalTags = focusEnabled
        ? normalizeFocusTags([
            ...focusTags,
            focusTagInput.trim().replace(/\s+/g, " "),
          ])
        : [];

      setIsSavingFocus(true);
      setFocusError(null);
      try {
        await updateFocus({
          id: entry.id,
          focused: focusEnabled,
          tags: finalTags,
        });
        setFocusTags(finalTags);
        setFocusTagInput("");
        setFocusOpen(false);
      } catch {
        setFocusError(t("entry.focus_save_failed"));
      } finally {
        setIsSavingFocus(false);
      }
    },
    [entry, focusEnabled, focusTagInput, focusTags, isSavingFocus, t, updateFocus],
  );

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
        onOpenFocusDialog={handleOpenFocus}
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
      <Dialog open={focusOpen} onOpenChange={handleFocusOpenChange}>
        <DialogContent className="max-w-md p-0">
          <DialogHeader className="border-b border-border px-4 py-3">
            <DialogTitle>{t("entry.focus_title")}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleFocusSubmit} className="space-y-4 p-4">
            <div className="flex items-start justify-between gap-4 rounded-xl border border-border/70 bg-muted/40 px-4 py-3">
              <div className="space-y-1">
                <div className="text-sm font-medium text-foreground">
                  {t("entry.focus_enabled_label")}
                </div>
                <p className="text-xs leading-5 text-muted-foreground">
                  {t("entry.focus_description")}
                </p>
              </div>
              <Switch checked={focusEnabled} onCheckedChange={setFocusEnabled} />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">
                {t("entry.focus_tags_label")}
              </label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={focusTagInput}
                  onChange={(e) => setFocusTagInput(e.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      handleAddFocusTag();
                    }
                  }}
                  placeholder={t("entry.focus_tags_placeholder")}
                  className={cn(
                    "flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm",
                    "focus:outline-none focus:ring-2 focus:ring-primary/50",
                    "placeholder:text-muted-foreground",
                    !focusEnabled && "cursor-not-allowed opacity-60",
                  )}
                  autoFocus
                  disabled={!focusEnabled}
                />
                <button
                  type="button"
                  onClick={handleAddFocusTag}
                  disabled={!focusEnabled}
                  className={cn(
                    "rounded-md border border-border bg-background px-3 py-2 text-sm font-medium transition-colors",
                    "hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50",
                  )}
                >
                  {t("entry.focus_add_tag")}
                </button>
              </div>
              <div className="flex flex-wrap gap-2">
                {focusTags.length > 0 ? (
                  focusTags.map((tag) => (
                    <button
                      key={tag}
                      type="button"
                      onClick={() => handleRemoveFocusTag(tag)}
                      className="inline-flex items-center gap-1 rounded-full border border-primary/20 bg-primary/5 px-3 py-1 text-xs font-medium text-primary transition-colors hover:bg-primary/10"
                    >
                      <span>{tag}</span>
                      <span aria-hidden="true">×</span>
                    </button>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground">
                    {t("entry.focus_tags_empty")}
                  </p>
                )}
              </div>
            </div>
            {focusError && (
              <div className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {focusError}
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={handleFocusClose}
                className={cn(
                  "rounded-md px-4 py-2 text-sm font-medium transition-colors",
                  "border border-border bg-background hover:bg-muted",
                )}
              >
                {t("actions.cancel")}
              </button>
              <button
                type="submit"
                disabled={isSavingFocus}
                className={cn(
                  "rounded-md px-4 py-2 text-sm font-medium transition-colors",
                  "bg-primary text-primary-foreground hover:bg-primary/90",
                  "disabled:cursor-not-allowed disabled:opacity-50",
                )}
              >
                {isSavingFocus ? t("entry.focus_saving") : t("entry.focus_save")}
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
