import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import {
  streamTranslateBlocks,
  isTranslateInit,
  isTranslateBlockResult,
  isTranslateDone,
  isTranslateError,
  type TranslateBlockData,
} from '@/api'
import { needsTranslation } from '@/lib/language-detect'
import { stripHtml } from '@/lib/html-utils'
import { useTranslationStore, translationActions } from '@/stores/translation-store'
import { translateArticlesBatch } from '@/services/translation-service'
import type { Entry } from '@/types/api'

interface UseAITranslationOptions {
  entry: Entry | undefined
  isReadableActive: boolean
  readableContent: string | null | undefined
  /** Auto-translate full content (detail view). */
  autoTranslate: boolean
  /** Auto-translate titles (list + detail header). */
  autoTranslateTitle: boolean
  targetLanguage: string
}

interface UseAITranslationReturn {
  isTranslating: boolean
  hasTranslation: boolean
  translationDisabled: boolean
  displayTitle: string | null
  translatedTitle: string | null
  /**
   * Bilingual blocks: always include original blocks; when translated text for a
   * block arrives, render it under the original block.
   */
  translatedContentBlocks: Array<{ key: string; html: string }> | null
  handleToggleTranslation: () => Promise<void>
}

export function useAITranslation({
  entry,
  isReadableActive,
  readableContent,
  autoTranslate,
  autoTranslateTitle,
  targetLanguage,
}: UseAITranslationOptions): UseAITranslationReturn {
  const [originalBlocks, setOriginalBlocks] = useState<TranslateBlockData[]>([])
  const [translatedBlocks, setTranslatedBlocks] = useState<Map<number, string>>(new Map())
  const [isTranslating, setIsTranslating] = useState(false)
  const [translationMode, setTranslationMode] = useState<boolean | null>(null)

  const translateAbortRef = useRef<AbortController | null>(null)
  const translateRequestedRef = useRef(false)
  const prevTranslateReadableRef = useRef(false)
  const manuallyDisabledRef = useRef(false)
  const requestSeqRef = useRef(0)
  const hasExistingTranslationRef = useRef(false)
  const isTranslatingRef = useRef(false)

  const clearTranslationState = useCallback(() => {
    setOriginalBlocks([])
    setTranslatedBlocks(new Map())
    setTranslationMode(null)
    hasExistingTranslationRef.current = false
  }, [])

  const cancelInFlightTranslation = useCallback(() => {
    requestSeqRef.current += 1
    if (translateAbortRef.current) {
      translateAbortRef.current.abort()
      translateAbortRef.current = null
    }
    setIsTranslating(false)
    isTranslatingRef.current = false
  }, [])

  // Reset state when entry changes
  useEffect(() => {
    cancelInFlightTranslation()
    clearTranslationState()
    translateRequestedRef.current = false
    prevTranslateReadableRef.current = false
    manuallyDisabledRef.current = false
  }, [entry?.id, cancelInFlightTranslation, clearTranslationState])

  const cachedTitleTranslation = useTranslationStore((state) =>
    entry ? state.getTranslation(entry.id, targetLanguage) : undefined
  )

  const isTranslationForCurrentMode = translationMode === isReadableActive

  const isAlreadyTargetLanguage = useMemo(() => {
    if (!entry) return false
    const content = isReadableActive ? readableContent : entry.content
    const summary = content ? stripHtml(content).slice(0, 200) : null
    return !needsTranslation(entry.title || '', summary, targetLanguage)
  }, [entry, isReadableActive, readableContent, targetLanguage])

  const displayTitle = useMemo(() => {
    return entry?.title ?? null
  }, [entry])

  const translatedTitle = useMemo(() => {
    if (!entry) return null

    // Show translated title when:
    // - user enabled auto title translation, OR
    // - content translation is currently active/in-progress (manual or auto)
    const shouldShowTranslatedTitle = autoTranslateTitle || translationMode !== null || isTranslating
    if (!shouldShowTranslatedTitle) return null

    const translated = cachedTitleTranslation?.title ?? null
    if (!translated) return null
    if ((entry.title ?? '') === translated) return null
    return translated
  }, [autoTranslateTitle, cachedTitleTranslation?.title, entry, isTranslating, translationMode])

  const translatedContentBlocks = useMemo(() => {
    if (!entry || !isTranslationForCurrentMode || originalBlocks.length === 0) {
      return null
    }
    const modeKey = translationMode ? 'readability' : 'normal'
    const blocks: Array<{ key: string; html: string }> = []

    for (const block of originalBlocks) {
      blocks.push({
        key: `${entry.id}-${modeKey}-${block.index}-orig`,
        html: block.html,
      })

      if (block.needTranslate) {
        const translatedHtml = translatedBlocks.get(block.index)
        if (translatedHtml) {
          blocks.push({
            key: `${entry.id}-${modeKey}-${block.index}-translated`,
            html: `<div class="mt-2 mb-4 rounded-md border-l-2 border-primary/30 bg-muted/30 px-4 py-2 text-muted-foreground [&_*]:text-muted-foreground">${translatedHtml}</div>`,
          })
        }
      }
    }

    return blocks
  }, [entry, isTranslationForCurrentMode, originalBlocks, translatedBlocks, translationMode])

  const hasTranslation = isTranslationForCurrentMode && originalBlocks.length > 0

  const generateTranslation = useCallback(async (forReadability: boolean) => {
    if (!entry) return

    const content = forReadability ? readableContent : entry.content
    if (!content) return

    requestSeqRef.current += 1
    if (translateAbortRef.current) {
      translateAbortRef.current.abort()
      translateAbortRef.current = null
    }
    const requestSeq = requestSeqRef.current

    setIsTranslating(true)
    isTranslatingRef.current = true
    setOriginalBlocks([])
    setTranslatedBlocks(new Map())
    hasExistingTranslationRef.current = false
    setTranslationMode(forReadability)
    translateRequestedRef.current = true

    const abortController = new AbortController()
    translateAbortRef.current = abortController

    try {
      const stream = streamTranslateBlocks(
        {
          entryId: entry.id,
          content,
          title: entry.title ?? undefined,
          isReadability: forReadability,
          returnBlocks: true,
        },
        abortController.signal
      )

      for await (const event of stream) {
        if (requestSeqRef.current !== requestSeq) {
          return
        }

        if ('cached' in event) {
          // Back-compat fallback: server returned cached full content JSON
          // (e.g. older server without `returnBlocks` support). Render as a
          // single bilingual pair: original article + translated article.
          setOriginalBlocks([{ index: 0, html: content, needTranslate: true }])
          setTranslatedBlocks(new Map([[0, event.content]]))
          break
        }

        if (isTranslateInit(event)) {
          setOriginalBlocks(event.blocks)
          continue
        }

        if (isTranslateBlockResult(event)) {
          setTranslatedBlocks(prev => {
            const newMap = new Map(prev)
            newMap.set(event.index, event.html)
            return newMap
          })
        }

        if (isTranslateDone(event)) {
          // Translation complete
        }

        if (isTranslateError(event)) {
          // Handle error
        }
      }
    } catch (err) {
      if (requestSeqRef.current !== requestSeq) {
        return
      }
      if (err instanceof Error && err.name === 'AbortError') {
        return
      }
      return
    } finally {
      if (requestSeqRef.current === requestSeq) {
        setIsTranslating(false)
        isTranslatingRef.current = false
        if (translateAbortRef.current === abortController) {
          translateAbortRef.current = null
        }
      }
    }
  }, [entry, readableContent])

  useEffect(() => {
    hasExistingTranslationRef.current = originalBlocks.length > 0
  }, [originalBlocks.length])

  useEffect(() => {
    isTranslatingRef.current = isTranslating
  }, [isTranslating])

  const handleToggleTranslation = useCallback(async () => {
    if (!entry) return

    if (hasTranslation && !isTranslating) {
      clearTranslationState()
      translateRequestedRef.current = false
      manuallyDisabledRef.current = true
      translationActions.disable(entry.id)
      return
    }

    if (isTranslating && translateAbortRef.current) {
      cancelInFlightTranslation()
      clearTranslationState()
      translateRequestedRef.current = false
      manuallyDisabledRef.current = true
      translationActions.disable(entry.id)
      return
    }

    manuallyDisabledRef.current = false
    translationActions.enable(entry.id)

    const summary = entry.content ? stripHtml(entry.content).slice(0, 200) : null
    if (needsTranslation(entry.title || '', summary, targetLanguage)) {
      translateArticlesBatch(
        [{ id: entry.id, title: entry.title || '', summary }],
        targetLanguage,
        { translateSummary: false }
      ).catch(() => {})
    }

    await generateTranslation(isReadableActive)
  }, [entry, hasTranslation, isTranslating, isReadableActive, clearTranslationState, cancelInFlightTranslation, generateTranslation, targetLanguage])

  // Auto-regenerate when readability mode changes
  useEffect(() => {
    if (prevTranslateReadableRef.current !== isReadableActive) {
      prevTranslateReadableRef.current = isReadableActive
      if (translateRequestedRef.current && (hasExistingTranslationRef.current || isTranslatingRef.current)) {
        const content = isReadableActive ? readableContent : entry?.content
        const summary = content ? stripHtml(content).slice(0, 200) : null

        if (needsTranslation(entry?.title || '', summary, targetLanguage)) {
          generateTranslation(isReadableActive)
        } else {
          cancelInFlightTranslation()
          clearTranslationState()
          translateRequestedRef.current = false
        }
      }
    }
  }, [
    isReadableActive,
    cancelInFlightTranslation,
    clearTranslationState,
    generateTranslation,
    readableContent,
    entry,
    targetLanguage,
  ])

  // Auto-translate when entry is selected
  useEffect(() => {
    if (!autoTranslate || !entry || isTranslating || isTranslatingRef.current) return
    if (manuallyDisabledRef.current) return
    if (hasTranslation) return

    const content = isReadableActive ? readableContent : entry.content
    const summary = content ? stripHtml(content).slice(0, 200) : null
    if (!needsTranslation(entry.title || '', summary, targetLanguage)) {
      return
    }

    generateTranslation(isReadableActive)
  }, [
    autoTranslate,
    entry,
    isReadableActive,
    readableContent,
    targetLanguage,
    isTranslating,
    hasTranslation,
    generateTranslation,
  ])

  // Auto-translate title when enabled (independent of content translation).
  useEffect(() => {
    if (!autoTranslateTitle || !entry) return
    if (translationActions.isDisabled(entry.id)) return
    if (cachedTitleTranslation?.title) return

    const content = isReadableActive ? readableContent : entry.content
    const summary = content ? stripHtml(content).slice(0, 200) : null
    if (!needsTranslation(entry.title || '', summary, targetLanguage)) return

    translateArticlesBatch(
      [{ id: entry.id, title: entry.title || '', summary }],
      targetLanguage,
      { translateSummary: false }
    ).catch(() => {})
  }, [autoTranslateTitle, cachedTitleTranslation?.title, entry, isReadableActive, readableContent, targetLanguage])

  return {
    isTranslating,
    hasTranslation,
    translationDisabled: isAlreadyTargetLanguage,
    displayTitle,
    translatedTitle,
    translatedContentBlocks,
    handleToggleTranslation,
  }
}
