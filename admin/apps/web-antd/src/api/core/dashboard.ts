import type {
  DashboardOverview,
  DashboardOverviewGranularity,
  DashboardOverviewPreset,
  DashboardSummary,
} from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type {
  DashboardOverview,
  DashboardOverviewGranularity,
  DashboardOverviewPreset,
  DashboardSummary,
} from '@vben/api-client';

export interface DashboardOverviewQuery {
  from?: string;
  granularity?: DashboardOverviewGranularity;
  preset?: DashboardOverviewPreset;
  timezone?: string;
  to?: string;
}

export function getDashboardSummaryApi() {
  return requestClient.get<DashboardSummary>(
    ADMIN_ENDPOINTS.getDashboardSummary,
  );
}

export function getDashboardOverviewApi(params?: DashboardOverviewQuery) {
  return requestClient.get<DashboardOverview>(
    ADMIN_ENDPOINTS.getDashboardOverview,
    { params },
  );
}
