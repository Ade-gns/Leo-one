/**
 * useFiles.ts — Hooks React Query pour la bibliothèque de fichiers déployables
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { filesApi } from '@/api/files'
import type { DeployFilePayload } from '@/types/file'

export const fileKeys = {
  all:  ['files'] as const,
  list: () => [...fileKeys.all, 'list'] as const,
}

/** Liste des fichiers de la bibliothèque du tenant courant. */
export function useFiles() {
  return useQuery({
    queryKey: fileKeys.list(),
    queryFn:  () => filesApi.list(),
    staleTime: 60_000,
  })
}

/** Mutation : upload d'un nouveau fichier. */
export function useUploadFile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ file, name }: { file: globalThis.File; name?: string }) => filesApi.upload(file, name),
    onSuccess:  () => qc.invalidateQueries({ queryKey: fileKeys.all }),
  })
}

/** Mutation : suppression d'un fichier. */
export function useDeleteFile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (fileID: string) => filesApi.delete(fileID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: fileKeys.all }),
  })
}

/** Mutation : déploiement d'un fichier de la bibliothèque sur un agent. */
export function useDeployFile(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: DeployFilePayload) => filesApi.deployFile(agentID, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents', 'commands', agentID] })
    },
  })
}
