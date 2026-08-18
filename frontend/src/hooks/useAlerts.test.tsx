import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import { useAlerts, useAcknowledgeAlert } from './useAlerts'
import type { Alert } from '@/types/alert'

vi.mock('@/api/alerts', () => ({
  alertsApi: {
    list:        vi.fn(),
    acknowledge: vi.fn(),
  },
}))

import { alertsApi } from '@/api/alerts'

const ALERT: Alert = {
  id: 'al1', tenant_id: 't1', agent_id: 'a1', agent_hostname: 'PARIS-01',
  severity: 'critical', status: 'open', title: 'CPU critique',
  triggered_at: '2024-01-01T00:00:00Z', created_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useAlerts', () => {
  it('retourne la liste des alertes', async () => {
    vi.mocked(alertsApi.list).mockResolvedValue({ data: [ALERT] })

    const { result } = renderHook(() => useAlerts(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([ALERT])
  })

  it('transmet le filtre à alertsApi.list', async () => {
    vi.mocked(alertsApi.list).mockResolvedValue({ data: [] })
    const filter = { severity: 'critical' as const }

    const { result } = renderHook(() => useAlerts(filter), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(alertsApi.list).toHaveBeenCalledWith(filter)
  })
})

describe('useAcknowledgeAlert', () => {
  it('appelle alertsApi.acknowledge avec l\'ID de l\'alerte', async () => {
    vi.mocked(alertsApi.acknowledge).mockResolvedValue({ data: { ...ALERT, status: 'acknowledged' } })

    const { result } = renderHook(() => useAcknowledgeAlert(), { wrapper: createWrapper() })
    result.current.mutate('al1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(alertsApi.acknowledge).toHaveBeenCalledWith('al1')
    expect(result.current.data?.data.status).toBe('acknowledged')
  })

  it("expose l'erreur si l'acquittement échoue", async () => {
    vi.mocked(alertsApi.acknowledge).mockRejectedValue(new Error('alerte introuvable'))

    const { result } = renderHook(() => useAcknowledgeAlert(), { wrapper: createWrapper() })
    result.current.mutate('inconnue')

    await waitFor(() => expect(result.current.isError).toBe(true))
  })
})
