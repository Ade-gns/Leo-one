import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import {
  useScripts, useCreateScript, useUpdateScript, useDeleteScript,
  useSchedules, useCreateSchedule,
} from './useScripts'
import type { Script, ScriptSchedule } from '@/types/script'

vi.mock('@/api/scripts', () => ({
  scriptsApi: {
    list:   vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
  schedulesApi: {
    list:   vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
}))

import { scriptsApi, schedulesApi } from '@/api/scripts'

const SCRIPT: Script = {
  id: 's1', name: 'Nettoyage', interpreter: 'bash', content: 'echo hi',
  created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

const SCHEDULE: ScriptSchedule = {
  id: 'sc1', script_id: 's1', name: 'Nocturne', agent_id: 'a1',
  cron_expression: '0 2 * * *', timeout_sec: 60, enabled: true,
  next_run_at: '2024-01-02T02:00:00Z', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useScripts', () => {
  it('retourne la liste des scripts depuis scriptsApi.list', async () => {
    vi.mocked(scriptsApi.list).mockResolvedValue({ data: [SCRIPT] })

    const { result } = renderHook(() => useScripts(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([SCRIPT])
    expect(scriptsApi.list).toHaveBeenCalledTimes(1)
  })

  it('expose une erreur quand scriptsApi.list échoue', async () => {
    vi.mocked(scriptsApi.list).mockRejectedValue(new Error('boom'))

    const { result } = renderHook(() => useScripts(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isError).toBe(true))
  })
})

describe('useCreateScript', () => {
  it('appelle scriptsApi.create avec le payload et invalide la liste', async () => {
    vi.mocked(scriptsApi.create).mockResolvedValue({ data: SCRIPT })
    const client = createWrapper()

    const { result } = renderHook(() => useCreateScript(), { wrapper: client })

    result.current.mutate({ name: 'Nettoyage', interpreter: 'bash', content: 'echo hi' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(scriptsApi.create).toHaveBeenCalledWith({ name: 'Nettoyage', interpreter: 'bash', content: 'echo hi' })
  })

  it("expose l'erreur sans lancer d'exception non gérée quand create échoue", async () => {
    vi.mocked(scriptsApi.create).mockRejectedValue(new Error('nom déjà pris'))

    const { result } = renderHook(() => useCreateScript(), { wrapper: createWrapper() })
    result.current.mutate({ name: 'Nettoyage', interpreter: 'bash', content: 'echo hi' })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toBe('nom déjà pris')
  })
})

describe('useUpdateScript', () => {
  it('appelle scriptsApi.update avec scriptID et payload', async () => {
    vi.mocked(scriptsApi.update).mockResolvedValue({ data: SCRIPT })

    const { result } = renderHook(() => useUpdateScript(), { wrapper: createWrapper() })
    result.current.mutate({ scriptID: 's1', payload: { content: 'echo bye' } })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(scriptsApi.update).toHaveBeenCalledWith('s1', { content: 'echo bye' })
  })
})

describe('useDeleteScript', () => {
  it('invalide aussi les planifications (cascade côté serveur)', async () => {
    vi.mocked(scriptsApi.delete).mockResolvedValue(undefined)
    vi.mocked(schedulesApi.list).mockResolvedValue({ data: [SCHEDULE] })

    const wrapper = createWrapper()
    const { result: schedules } = renderHook(() => useSchedules(), { wrapper })
    await waitFor(() => expect(schedules.current.isSuccess).toBe(true))

    const { result: del } = renderHook(() => useDeleteScript(), { wrapper })
    del.current.mutate('s1')

    await waitFor(() => expect(del.current.isSuccess).toBe(true))
    expect(scriptsApi.delete).toHaveBeenCalledWith('s1')
  })
})

describe('useSchedules / useCreateSchedule', () => {
  it('liste les planifications', async () => {
    vi.mocked(schedulesApi.list).mockResolvedValue({ data: [SCHEDULE] })

    const { result } = renderHook(() => useSchedules(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([SCHEDULE])
  })

  it('crée une planification avec le payload fourni', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue({ data: SCHEDULE })

    const { result } = renderHook(() => useCreateSchedule(), { wrapper: createWrapper() })
    const payload = { script_id: 's1', name: 'Nocturne', agent_id: 'a1', cron_expression: '0 2 * * *' }
    result.current.mutate(payload)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(schedulesApi.create).toHaveBeenCalledWith(payload)
  })
})
