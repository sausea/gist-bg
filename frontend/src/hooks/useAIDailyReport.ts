import { useQuery } from "@tanstack/react-query";
import { getAIDailyReport } from "@/api";

export function useAIDailyReport(date: string) {
  return useQuery({
    queryKey: ["aiDailyReport", date],
    queryFn: ({ signal }) => getAIDailyReport(date, signal),
  });
}
