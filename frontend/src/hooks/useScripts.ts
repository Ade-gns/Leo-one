/**
 * useScripts.ts — Hooks React Query pour la bibliothèque de scripts et
 * leurs planifications récurrentes
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { scriptsApi, schedulesApi } from '@/api/scripts'
import type {
  CreateScriptPayload, UpdateScriptPayload,
  CreateSchedulePayload, UpdateSchedulePayload,
} from '@/types/script'

export const scriptKeys = {
  all:  ['scripts'] as const,
  list: () => [...scriptKeys.all, 'list'] as const,
}

export const scheduleKeys = {
  all:  ['script-schedules'] as const,
  list: () => [...scheduleKeys.all, 'list'] as const,
}

/** Liste des scripts de la bibliothèque du tenant courant. */
export function useScripts() {
  return useQuery({
    queryKey: scriptKeys.list(),
    queryFn:  () => scriptsApi.list(),
    staleTime: 60_000,
  })
}

export function useCreateScript() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateScriptPayload) => scriptsApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: scriptKeys.all }),
  })
}

export function useUpdateScript() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ scriptID, payload }: { scriptID: string; payload: UpdateScriptPayload }) =>
      scriptsApi.update(scriptID, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: scriptKeys.all }),
  })
}

export function useDeleteScript() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (scriptID: string) => scriptsApi.delete(scriptID),
    onSuccess:  () => {
      qc.invalidateQueries({ queryKey: scriptKeys.all })
      // Les planifications référençant ce script sont supprimées en cascade
      // côté serveur (script_schedules.script_id ON DELETE CASCADE).
      qc.invalidateQueries({ queryKey: scheduleKeys.all })
    },
  })
}

/** Liste des planifications récurrentes du tenant courant. */
export function useSchedules() {
  return useQuery({
    queryKey: scheduleKeys.list(),
    queryFn:  () => schedulesApi.list(),
    staleTime: 30_000,
  })
}

export function useCreateSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateSchedulePayload) => schedulesApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: scheduleKeys.all }),
  })
}

export function useUpdateSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ scheduleID, payload }: { scheduleID: string; payload: UpdateSchedulePayload }) =>
      schedulesApi.update(scheduleID, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: scheduleKeys.all }),
  })
}

export function useDeleteSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (scheduleID: string) => schedulesApi.delete(scheduleID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: scheduleKeys.all }),
  })
}
