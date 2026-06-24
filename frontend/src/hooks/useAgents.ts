/**
 * useAgents.ts — Hooks React Query pour les agents
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { agentsApi } from '@/api/agents'
import type { AgentListFilter } from '@/types/agent'
import type { ExecScriptPayload } from '@/types/agent'

export const agentKeys = {
  all:     ['agents'] as const,
  list:    (filter?: AgentListFilter) => [...agentKeys.all, 'list', filter] as const,
  detail:  (id: string)              => [...agentKeys.all, 'detail', id] as const,
  hw:      (id: string)              => [...agentKeys.all, 'hw-inventory', id] as const,
  sw:      (id: string)              => [...agentKeys.all, 'sw-inventory', id] as const,
  commands:(id: string)              => [...agentKeys.all, 'commands', id] as const,
}

/** Liste des agents avec filtres optionnels */
export function useAgents(filter?: AgentListFilter) {
  return useQuery({
    queryKey: agentKeys.list(filter),
    queryFn:  () => agentsApi.list(filter),
    staleTime: 30_000,  /* 30s — pas besoin de rafraîchir trop souvent */
  })
}

/** Détail d'un agent */
export function useAgent(agentID: string) {
  return useQuery({
    queryKey: agentKeys.detail(agentID),
    queryFn:  () => agentsApi.get(agentID),
    enabled:  !!agentID,
    staleTime: 15_000,
  })
}

/** Mutation : exécution de script sur un agent */
export function useExecScript(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: ExecScriptPayload) => agentsApi.execScript(agentID, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentKeys.commands(agentID) })
    },
  })
}

/** Historique des commandes d'un agent */
export function useAgentCommands(agentID: string) {
  return useQuery({
    queryKey: agentKeys.commands(agentID),
    queryFn:  () => agentsApi.listCommands(agentID),
    enabled:  !!agentID,
    refetchInterval: 5_000,  /* rafraîchi toutes les 5s quand une commande est en cours */
  })
}

/** Inventaire matériel */
export function useHardwareInventory(agentID: string) {
  return useQuery({
    queryKey: agentKeys.hw(agentID),
    queryFn:  () => agentsApi.getHardwareInventory(agentID),
    enabled:  !!agentID,
    staleTime: 5 * 60_000,  /* 5 min — l'inventaire HW change rarement */
  })
}

/** Mutation : suppression d'un agent */
export function useDeleteAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (agentID: string) => agentsApi.delete(agentID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: agentKeys.all }),
  })
}
