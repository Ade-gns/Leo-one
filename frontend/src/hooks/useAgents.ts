/**
 * useAgents.ts — Hooks React Query pour les agents
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { agentsApi } from '@/api/agents'
import type { AgentListFilter } from '@/types/agent'
import type { ExecScriptPayload, InstallPkgPayload, BulkExecScriptPayload } from '@/types/agent'

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

/** Mutation : exécution d'un script sur plusieurs agents (ou un workspace entier) à la fois */
export function useBulkExecScript() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: BulkExecScriptPayload) => agentsApi.bulkExecScript(payload),
    onSuccess: () => {
      // Chaque agent ciblé a potentiellement une nouvelle commande en cours —
      // invalider la liste globale (pas de clé par-agent connue à l'avance ici).
      qc.invalidateQueries({ queryKey: agentKeys.all })
    },
  })
}

/** Mutation : installation d'un ou plusieurs paquets sur un agent */
export function useInstallPkg(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: InstallPkgPayload) => agentsApi.installPkg(agentID, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentKeys.commands(agentID) })
      // L'installation peut changer la liste des logiciels installés —
      // invalider pour que l'onglet Logiciels se resynchronise si l'agent
      // renvoie un COLLECT_INVENTORY après coup (pas automatique aujourd'hui,
      // mais évite d'afficher une liste obsolète si l'utilisateur la rouvre).
      qc.invalidateQueries({ queryKey: agentKeys.sw(agentID) })
    },
  })
}

/** Mutation : réveil manuel d'un agent */
export function useWakeAgent(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => agentsApi.wakeUp(agentID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentKeys.detail(agentID) })
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

/** Une commande précise, avec polling tant qu'elle n'est pas terminée —
 *  utilisé pour la barre de progression d'un déploiement de fichier
 *  (Command.progress_percent, alimenté par FILE_TRANSFER_PROGRESS côté
 *  agent). S'arrête de poller une fois status 'success'/'failed'/'timeout'. */
export function useAgentCommand(agentID: string, commandID: string | null) {
  return useQuery({
    queryKey: [...agentKeys.commands(agentID), commandID],
    queryFn:  () => agentsApi.getCommand(agentID, commandID!),
    enabled:  !!agentID && !!commandID,
    refetchInterval: query => {
      const status = query.state.data?.data.status
      return status === 'pending' || status === 'running' ? 1_500 : false
    },
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

/** Inventaire logiciel */
export function useSoftwareInventory(agentID: string, opts?: { enabled?: boolean; search?: string }) {
  return useQuery({
    queryKey: agentKeys.sw(agentID),
    queryFn:  () => agentsApi.getSoftwareInventory(agentID, opts?.search ? { search: opts.search } : undefined),
    enabled:  !!agentID && (opts?.enabled ?? true),
    staleTime: 60_000,
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
