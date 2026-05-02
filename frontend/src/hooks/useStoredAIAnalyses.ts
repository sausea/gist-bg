import { useQuery } from "@tanstack/react-query";
import { listStoredAIAnalyses } from "@/api";

interface UseStoredAIAnalysesOptions {
  limit?: number;
  offset?: number;
  refetchInterval?: number | false;
}

export function useStoredAIAnalyses({
  limit = 100,
  offset = 0,
  refetchInterval = false,
}: UseStoredAIAnalysesOptions = {}) {
  return useQuery({
    queryKey: ["storedAIAnalyses", limit, offset],
    queryFn: ({ signal }) => listStoredAIAnalyses(limit, offset, signal),
    refetchInterval,
    refetchIntervalInBackground: refetchInterval !== false,
  });
}
