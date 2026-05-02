import { useState, useCallback, useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { analyzeArticle, ApiError, type AIAnalysis } from '@/api'
import type { Entry } from '@/types/api'

interface UseAIAnalysisOptions {
  entry: Entry | undefined
  isReadableActive: boolean
  readableContent: string | null | undefined
  autoAnalysis: boolean
  backgroundProcessing: boolean
  backgroundStatusChecked: boolean
}

interface UseAIAnalysisReturn {
  aiAnalysis: AIAnalysis | null
  isLoadingAnalysis: boolean
  analysisError: string | null
  requestAnalysis: (forReadability?: boolean) => Promise<void>
  cancelAnalysis: () => void
}

export function useAIAnalysis({
  entry,
  isReadableActive,
  readableContent,
  autoAnalysis,
  backgroundProcessing,
  backgroundStatusChecked,
}: UseAIAnalysisOptions): UseAIAnalysisReturn {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  const [aiAnalysis, setAiAnalysis] = useState<AIAnalysis | null>(null)
  const [isLoadingAnalysis, setIsLoadingAnalysis] = useState(false)
  const [analysisError, setAnalysisError] = useState<string | null>(null)

  const analysisAbortRef = useRef<AbortController | null>(null)
  const analysisRequestedRef = useRef(false)
  const prevReadableActiveRef = useRef(false)
  const analysisManuallyDisabledRef = useRef(false)

  const markEntryAsAnalyzed = useCallback((entryID: string) => {
    queryClient.setQueryData<Entry>(['entry', entryID], (old) => {
      if (!old) return old
      return { ...old, hasAnalysis: true }
    })

    queryClient.setQueriesData<{ pages: { entries: Entry[] }[] }>(
      { queryKey: ['entries'] },
      (old) => {
        if (!old) return old
        return {
          ...old,
          pages: old.pages.map((page) => ({
            ...page,
            entries: page.entries.map((item) =>
              item.id === entryID ? { ...item, hasAnalysis: true } : item
            ),
          })),
        }
      }
    )

    queryClient.invalidateQueries({ queryKey: ['feedAIStats'] })
    queryClient.invalidateQueries({ queryKey: ['aiProcessingStatus', entryID] })
  }, [queryClient])

  useEffect(() => {
    // This hook mirrors the existing summary/translation lifecycle:
    // when the entry changes, any in-flight analysis is cancelled and local UI state resets.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setAiAnalysis(null)
    setAnalysisError(null)
    setIsLoadingAnalysis(false)
    if (analysisAbortRef.current) {
      analysisAbortRef.current.abort()
      analysisAbortRef.current = null
    }
    analysisRequestedRef.current = false
    prevReadableActiveRef.current = false
    analysisManuallyDisabledRef.current = false
  }, [entry?.id])

  const generateAnalysis = useCallback(async (forReadability: boolean) => {
    if (!entry) return

    if (analysisAbortRef.current) {
      analysisAbortRef.current.abort()
    }

    const content = forReadability ? readableContent : entry.content
    if (!content) {
      setAnalysisError('No content to analyze')
      return
    }

    setIsLoadingAnalysis(true)
    setAnalysisError(null)
    setAiAnalysis(null)
    analysisRequestedRef.current = true

    const abortController = new AbortController()
    analysisAbortRef.current = abortController

    try {
      const result = await analyzeArticle(
        {
          entryId: entry.id,
          content,
          title: entry.title ?? undefined,
          isReadability: forReadability,
        },
        abortController.signal
      )
      setAiAnalysis(result)
      markEntryAsAnalyzed(entry.id)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        return
      }
      const message = err instanceof ApiError && err.status === 404
        ? t('entry.analysis_endpoint_missing')
        : err instanceof Error
          ? err.message
          : t('entry.analysis_failed')
      setAnalysisError(message)
      setIsLoadingAnalysis(false)
      analysisAbortRef.current = null
      return
    }

    setIsLoadingAnalysis(false)
    analysisAbortRef.current = null
  }, [entry, readableContent, t, markEntryAsAnalyzed])

  const requestAnalysis = useCallback(async (forReadability?: boolean) => {
    analysisManuallyDisabledRef.current = false
    await generateAnalysis(forReadability ?? isReadableActive)
  }, [generateAnalysis, isReadableActive])

  const cancelAnalysis = useCallback(() => {
    if (analysisAbortRef.current) {
      analysisAbortRef.current.abort()
      analysisAbortRef.current = null
    }
    setIsLoadingAnalysis(false)
    analysisRequestedRef.current = false
    analysisManuallyDisabledRef.current = true
  }, [])

  useEffect(() => {
    if (prevReadableActiveRef.current !== isReadableActive) {
      prevReadableActiveRef.current = isReadableActive
      if (analysisRequestedRef.current && (aiAnalysis || isLoadingAnalysis)) {
        // eslint-disable-next-line react-hooks/set-state-in-effect
        generateAnalysis(isReadableActive)
      }
    }
  }, [isReadableActive, aiAnalysis, isLoadingAnalysis, generateAnalysis])

  useEffect(() => {
    if (!autoAnalysis || !entry || isLoadingAnalysis) return
    if (!backgroundStatusChecked) return
    if (backgroundProcessing) return
    if (analysisManuallyDisabledRef.current) return
    if (aiAnalysis || analysisRequestedRef.current) return

    generateAnalysis(isReadableActive)
  }, [
    autoAnalysis,
    entry,
    isReadableActive,
    isLoadingAnalysis,
    aiAnalysis,
    generateAnalysis,
    backgroundProcessing,
    backgroundStatusChecked,
  ])

  return {
    aiAnalysis,
    isLoadingAnalysis,
    analysisError,
    requestAnalysis,
    cancelAnalysis,
  }
}
