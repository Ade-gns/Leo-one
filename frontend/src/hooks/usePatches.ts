/**
 * usePatches.ts — Hooks React Query pour la gestion des mises à jour
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { patchesApi } from '@/api/patches'
import type { InstallPatchesPayload, BulkInstallPatchesPayload } from '@/types/patch'

export const patchKeys = {
  all:     ['patches'] as const,
  list:    (agentID: string) => [...patchKeys.all, 'list', agentID] as const,
  summary: ()               => [...patchKeys.all, 'summary'] as const,
}

/** Liste des patchs connus pour un agent. */
export function usePatches(agentID: string) {
  return useQuery({
    queryKey: patchKeys.list(agentID),
    queryFn:  () => patchesApi.list(agentID),
    enabled:  !!agentID,
    staleTime: 30_000,
  })
}

/** Résumé des patchs en attente pour le tenant courant (widget dashboard). */
export function usePatchesSummary() {
  return useQuery({
    queryKey: patchKeys.summary(),
    queryFn:  () => patchesApi.summary(),
    staleTime: 60_000,
  })
}

/** Mutation : installation d'une sélection de patchs sur un agent. */
export function useInstallPatches(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: InstallPatchesPayload) => patchesApi.install(agentID, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: patchKeys.list(agentID) })
      qc.invalidateQueries({ queryKey: patchKeys.summary() })
    },
  })
}

/** Mutation : installation groupée sur plusieurs agents ou un workspace entier. */
export function useBulkInstallPatches() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: BulkInstallPatchesPayload) => patchesApi.bulkInstall(payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: patchKeys.all })
    },
  })
}
