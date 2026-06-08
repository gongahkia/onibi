import { useQuery, type UseQueryOptions } from "@tanstack/react-query";
import {
  fetchApprovalHistory,
  type ApprovalAuditRecord,
  type ApprovalHistoryOptions,
} from "./approval-audit";
import { fetchConfigStatus, type ConfigStatusResponse } from "./config-status";
import { appQueryClient } from "./query-client";
import { fetchTransportStatus, type TransportSnapshot } from "./transports";

type QueryOptions<T> = Omit<UseQueryOptions<T, Error>, "queryKey" | "queryFn">;

export const queryKeys = {
  approvalHistory: (options: ApprovalHistoryOptions) => ["approval-history", options] as const,
  configStatus: ["config-status"] as const,
  transportStatus: ["transport-status"] as const,
};

export function useApprovalHistoryQuery(
  options: ApprovalHistoryOptions = {},
  queryOptions: QueryOptions<ApprovalAuditRecord[]> = {},
) {
  return useQuery<ApprovalAuditRecord[], Error>(
    {
      queryKey: queryKeys.approvalHistory(options),
      queryFn: () => fetchApprovalHistory(options),
      ...queryOptions,
    },
    appQueryClient,
  );
}

export function useConfigStatusQuery(
  queryOptions: QueryOptions<ConfigStatusResponse> = {},
) {
  return useQuery<ConfigStatusResponse, Error>(
    {
      queryKey: queryKeys.configStatus,
      queryFn: fetchConfigStatus,
      ...queryOptions,
    },
    appQueryClient,
  );
}

export function useTransportStatusQuery(
  queryOptions: QueryOptions<TransportSnapshot[]> = {},
) {
  return useQuery<TransportSnapshot[], Error>(
    {
      queryKey: queryKeys.transportStatus,
      queryFn: fetchTransportStatus,
      ...queryOptions,
    },
    appQueryClient,
  );
}
