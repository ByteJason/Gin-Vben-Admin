import type { MonitorOverview } from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type { MonitorOverview } from '@vben/api-client';

export function getMonitorOverviewApi() {
  return requestClient.get<MonitorOverview>(ADMIN_ENDPOINTS.getMonitorOverview);
}

export const refreshMonitorOverviewApi = getMonitorOverviewApi;
