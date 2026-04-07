import { useQuery } from "@tanstack/react-query";
import { getAIProcessingStatus } from "@/api";

interface UseAIProcessingStatusOptions {
  entryId: string | null | undefined;
  enabled?: boolean;
}

export function useAIProcessingStatus({
  entryId,
  enabled = true,
}: UseAIProcessingStatusOptions) {
  return useQuery({
    queryKey: ["aiProcessingStatus", entryId],
    queryFn: ({ signal }) => getAIProcessingStatus(entryId as string, signal),
    enabled: enabled && !!entryId,
    refetchInterval: (query) => {
      const data = query.state.data;
      if (!enabled || !entryId) return false;
      return !data || data.processing ? 3000 : false;
    },
    staleTime: 0,
  });
}
