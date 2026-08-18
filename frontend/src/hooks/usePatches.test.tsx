import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import { usePatches, usePatchesSummary, useInstallPatches, useBulkInstallPatches } from './usePatches'
import type { Patch, PatchesSummary } from '@/types/patch'

vi.mock('@/api/patches', () => ({
  patchesApi: {
    list:        vi.fn(),
    summary:     vi.fn(),
    install:     vi.fn(),
    bulkInstall: vi.fn(),
  },
}))

import { patchesApi } from '@/api/patches'

const PATCH: Patch = {
  id: 'p1', tenant_id: 't1', agent_id: 'a1', native_id: 'bash', title: 'bash → 5.1.1',
  severity: 'important', status: 'available', detected_at: '2024-01-01T00:00:00Z',
}

const SUMMARY: PatchesSummary = {
  agents_with_critical_pending: 2, agents_with_pending_patches: 5, total_pending_patches: 9,
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('usePatches', () => {
  it('retourne la liste des patchs pour un agent', async () => {
    vi.mocked(patchesApi.list).mockResolvedValue({ data: [PATCH] })

    const { result } = renderHook(() => usePatches('a1'), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([PATCH])
    expect(patchesApi.list).toHaveBeenCalledWith('a1')
  })

  it("ne lance pas de requête si agentID est vide", () => {
    renderHook(() => usePatches(''), { wrapper: createWrapper() })
    expect(patchesApi.list).not.toHaveBeenCalled()
  })
})

describe('usePatchesSummary', () => {
  it('retourne le résumé du tenant', async () => {
    vi.mocked(patchesApi.summary).mockResolvedValue({ data: SUMMARY })

    const { result } = renderHook(() => usePatchesSummary(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual(SUMMARY)
  })
})

describe('useInstallPatches', () => {
  it('installe la sélection sur l\'agent donné', async () => {
    vi.mocked(patchesApi.install).mockResolvedValue({ data: { command_id: 'c1', status: 'pending', sent: true } })

    const { result } = renderHook(() => useInstallPatches('a1'), { wrapper: createWrapper() })
    result.current.mutate({ patch_ids: ['bash', 'curl'], reboot_after: true })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(patchesApi.install).toHaveBeenCalledWith('a1', { patch_ids: ['bash', 'curl'], reboot_after: true })
  })

  it("expose l'erreur si l'installation échoue", async () => {
    vi.mocked(patchesApi.install).mockRejectedValue(new Error('agent introuvable'))

    const { result } = renderHook(() => useInstallPatches('a1'), { wrapper: createWrapper() })
    result.current.mutate({ patch_ids: ['bash'] })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toBe('agent introuvable')
  })
})

describe('useBulkInstallPatches', () => {
  it('envoie le payload groupé tel quel', async () => {
    const results = [{ agent_id: 'a1', command_id: 'c1', sent: true }]
    vi.mocked(patchesApi.bulkInstall).mockResolvedValue({ data: results })

    const { result } = renderHook(() => useBulkInstallPatches(), { wrapper: createWrapper() })
    const payload = { agent_ids: ['a1', 'a2'], min_severity: 'critical' as const }
    result.current.mutate(payload)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(patchesApi.bulkInstall).toHaveBeenCalledWith(payload)
    expect(result.current.data?.data).toEqual(results)
  })
})
