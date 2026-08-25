import type { DashboardSummary } from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type { DashboardSummary } from '@vben/api-client';

export function getDashboardSummaryApi() {
  return requestClient.get<DashboardSummary>(
    ADMIN_ENDPOINTS.getDashboardSummary,
  );
}
