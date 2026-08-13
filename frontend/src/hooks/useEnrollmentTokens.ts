/**
 * useEnrollmentTokens.ts — Hooks React Query pour les tokens d'enrôlement
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { enrollmentTokensApi } from '@/api/enrollmentTokens'
import type { CreateEnrollmentTokenPayload } from '@/types/enrollmentToken'

export const enrollmentTokenKeys = {
  all:  ['enrollment-tokens'] as const,
  list: () => [...enrollmentTokenKeys.all, 'list'] as const,
}

/** Liste des tokens d'enrôlement du tenant courant */
export function useEnrollmentTokens(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: enrollmentTokenKeys.list(),
    queryFn:  () => enrollmentTokensApi.list(),
    enabled:  opts?.enabled ?? true,
    staleTime: 10_000,
  })
}

/** Mutation : génération d'un nouveau token d'enrôlement */
export function useCreateEnrollmentToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateEnrollmentTokenPayload) => enrollmentTokensApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: enrollmentTokenKeys.all }),
  })
}

/** Mutation : révocation (suppression) d'un token d'enrôlement */
export function useDeleteEnrollmentToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tokenID: string) => enrollmentTokensApi.delete(tokenID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: enrollmentTokenKeys.all }),
  })
}
