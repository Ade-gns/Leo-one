import { get, post, patch, del } from './client'
import type { ApiResponse } from '@/types/api'
import type {
  Script, CreateScriptPayload, UpdateScriptPayload,
  ScriptSchedule, CreateSchedulePayload, UpdateSchedulePayload,
} from '@/types/script'

const SCRIPTS_BASE    = '/api/v1/scripts'
const SCHEDULES_BASE  = '/api/v1/script-schedules'

export const scriptsApi = {
  list: () =>
    get<ApiResponse<Script[]>>(SCRIPTS_BASE),

  create: (payload: CreateScriptPayload) =>
    post<ApiResponse<Script>>(SCRIPTS_BASE, payload),

  update: (scriptID: string, payload: UpdateScriptPayload) =>
    patch<ApiResponse<Script>>(`${SCRIPTS_BASE}/${scriptID}`, payload),

  delete: (scriptID: string) =>
    del<void>(`${SCRIPTS_BASE}/${scriptID}`),
}

export const schedulesApi = {
  list: () =>
    get<ApiResponse<ScriptSchedule[]>>(SCHEDULES_BASE),

  create: (payload: CreateSchedulePayload) =>
    post<ApiResponse<ScriptSchedule>>(SCHEDULES_BASE, payload),

  update: (scheduleID: string, payload: UpdateSchedulePayload) =>
    patch<ApiResponse<ScriptSchedule>>(`${SCHEDULES_BASE}/${scheduleID}`, payload),

  delete: (scheduleID: string) =>
    del<void>(`${SCHEDULES_BASE}/${scheduleID}`),
}
