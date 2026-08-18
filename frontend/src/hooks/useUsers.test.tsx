import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import { useUsers, useCreateUser, useUpdateUser, useDeleteUser } from './useUsers'
import type { User } from '@/types/user'

vi.mock('@/api/users', () => ({
  usersApi: {
    list:   vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
}))

import { usersApi } from '@/api/users'

const USER: User = {
  id: 'u1', tenant_id: 't1', email: 'a@b.com', full_name: 'Alice',
  is_active: true, mfa_enabled: false, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useUsers', () => {
  it('retourne la liste des utilisateurs', async () => {
    vi.mocked(usersApi.list).mockResolvedValue({ data: [USER] })

    const { result } = renderHook(() => useUsers(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([USER])
  })
})

describe('useCreateUser', () => {
  it('appelle usersApi.create avec le payload', async () => {
    vi.mocked(usersApi.create).mockResolvedValue({ data: USER })
    const payload = { email: 'a@b.com', full_name: 'Alice', password: 'motdepasse123' }

    const { result } = renderHook(() => useCreateUser(), { wrapper: createWrapper() })
    result.current.mutate(payload)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(usersApi.create).toHaveBeenCalledWith(payload)
  })

  it("expose l'erreur si l'email existe déjà", async () => {
    vi.mocked(usersApi.create).mockRejectedValue(new Error('un utilisateur avec cet email existe déjà'))

    const { result } = renderHook(() => useCreateUser(), { wrapper: createWrapper() })
    result.current.mutate({ email: 'a@b.com', full_name: 'Alice', password: 'motdepasse123' })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toBe('un utilisateur avec cet email existe déjà')
  })
})

describe('useUpdateUser', () => {
  it('appelle usersApi.update avec userID et payload', async () => {
    vi.mocked(usersApi.update).mockResolvedValue({ data: { ...USER, is_active: false } })

    const { result } = renderHook(() => useUpdateUser(), { wrapper: createWrapper() })
    result.current.mutate({ userID: 'u1', payload: { is_active: false } })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(usersApi.update).toHaveBeenCalledWith('u1', { is_active: false })
  })
})

describe('useDeleteUser', () => {
  it('appelle usersApi.delete avec userID', async () => {
    vi.mocked(usersApi.delete).mockResolvedValue(undefined)

    const { result } = renderHook(() => useDeleteUser(), { wrapper: createWrapper() })
    result.current.mutate('u1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(usersApi.delete).toHaveBeenCalledWith('u1')
  })
})
