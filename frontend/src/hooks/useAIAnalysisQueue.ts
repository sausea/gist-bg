import { useQuery } from "@tanstack/react-query";
import { listAIAnalysisQueue } from "@/api";

interface UseAIAnalysisQueueOptions {
  limit?: number;
}

export function useAIAnalysisQueue({
  limit = 50,
}: UseAIAnalysisQueueOptions = {}) {
  return useQuery({
    queryKey: ["aiAnalysisQueue", limit],
    queryFn: ({ signal }) => listAIAnalysisQueue(limit, signal),
    refetchInterval: 5000,
    refetchIntervalInBackground: true,
  });
}
