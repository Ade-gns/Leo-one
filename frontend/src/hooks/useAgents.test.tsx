import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import { useAgents, useBulkExecScript, useDeleteAgent, useWakeAgent } from './useAgents'
import type { Agent, BulkCommandResult } from '@/types/agent'

vi.mock('@/api/agents', () => ({
  agentsApi: {
    list:           vi.fn(),
    delete:         vi.fn(),
    bulkExecScript: vi.fn(),
    wakeUp:         vi.fn(),
  },
}))

import { agentsApi } from '@/api/agents'

const AGENT: Agent = {
  id: 'a1', tenant_id: 't1', hostname: 'PARIS-01', os: 'linux', os_version: '24.04',
  arch: 'amd64', hardware_id: 'hw1', agent_version: '1.0.0', status: 'online',
  enrolled_at: '2024-01-01T00:00:00Z', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useAgents', () => {
  it('retourne la liste des agents', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue({ data: [AGENT] })

    const { result } = renderHook(() => useAgents(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([AGENT])
  })

  it('transmet le filtre à agentsApi.list', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue({ data: [] })
    const filter = { status: 'online' as const }

    const { result } = renderHook(() => useAgents(filter), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(agentsApi.list).toHaveBeenCalledWith(filter)
  })
})

describe('useBulkExecScript', () => {
  it('envoie le payload et retourne les résultats par agent', async () => {
    const results: BulkCommandResult[] = [
      { agent_id: 'a1', command_id: 'c1', sent: true },
      { agent_id: 'a2', sent: false, error: 'hors ligne' },
    ]
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: results })

    const { result } = renderHook(() => useBulkExecScript(), { wrapper: createWrapper() })
    result.current.mutate({ agent_ids: ['a1', 'a2'], interpreter: 'bash', script: 'echo hi', timeout_sec: 60 })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual(results)
  })

  it("expose l'erreur si l'envoi groupé échoue", async () => {
    vi.mocked(agentsApi.bulkExecScript).mockRejectedValue(new Error('aucun agent cible trouvé'))

    const { result } = renderHook(() => useBulkExecScript(), { wrapper: createWrapper() })
    result.current.mutate({ workspace_id: 'w1', interpreter: 'bash', script: 'echo hi', timeout_sec: 60 })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toBe('aucun agent cible trouvé')
  })
})

describe('useDeleteAgent', () => {
  it('appelle agentsApi.delete avec l\'ID', async () => {
    vi.mocked(agentsApi.delete).mockResolvedValue(undefined)

    const { result } = renderHook(() => useDeleteAgent(), { wrapper: createWrapper() })
    result.current.mutate('a1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(agentsApi.delete).toHaveBeenCalledWith('a1')
  })
})

describe('useWakeAgent', () => {
  it('appelle agentsApi.wakeUp pour cet agent', async () => {
    vi.mocked(agentsApi.wakeUp).mockResolvedValue({ data: { status: 'sent', message: 'ok', online: true } })

    const { result } = renderHook(() => useWakeAgent('a1'), { wrapper: createWrapper() })
    result.current.mutate()

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(agentsApi.wakeUp).toHaveBeenCalledWith('a1')
  })
})
