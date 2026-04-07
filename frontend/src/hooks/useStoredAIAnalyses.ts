import { useQuery } from "@tanstack/react-query";
import { listStoredAIAnalyses } from "@/api";

interface UseStoredAIAnalysesOptions {
  limit?: number;
  offset?: number;
}

export function useStoredAIAnalyses({
  limit = 100,
  offset = 0,
}: UseStoredAIAnalysesOptions = {}) {
  return useQuery({
    queryKey: ["storedAIAnalyses", limit, offset],
    queryFn: ({ signal }) => listStoredAIAnalyses(limit, offset, signal),
  });
}
