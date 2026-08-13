/**
 * useWorkspaces.ts — Hooks React Query pour les workspaces
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { workspacesApi } from '@/api/workspaces'
import type { CreateWorkspacePayload, UpdateWorkspacePayload } from '@/types/workspace'

export const workspaceKeys = {
  all:  ['workspaces'] as const,
  list: () => [...workspaceKeys.all, 'list'] as const,
}

/** Liste des workspaces du tenant courant */
export function useWorkspaces() {
  return useQuery({
    queryKey: workspaceKeys.list(),
    queryFn:  () => workspacesApi.list(),
    staleTime: 60_000,
  })
}

/** Mutation : création d'un workspace */
export function useCreateWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateWorkspacePayload) => workspacesApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: workspaceKeys.all }),
  })
}

/** Mutation : mise à jour partielle d'un workspace (name/description) */
export function useUpdateWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ workspaceID, payload }: { workspaceID: string; payload: UpdateWorkspacePayload }) =>
      workspacesApi.update(workspaceID, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: workspaceKeys.all }),
  })
}

/** Mutation : suppression d'un workspace (les agents rattachés sont détachés, pas supprimés) */
export function useDeleteWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (workspaceID: string) => workspacesApi.delete(workspaceID),
    onSuccess:  () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.all })
      // La suppression détache les agents rattachés (workspace_id → null) —
      // resynchronise la liste des machines pour ne pas afficher un
      // workspace_id obsolète.
      qc.invalidateQueries({ queryKey: ['agents'] })
    },
  })
}
