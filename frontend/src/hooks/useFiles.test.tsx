import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { createWrapper } from '@/test/renderWithClient'
import { useFiles, useUploadFile, useDeleteFile, useDeployFile } from './useFiles'
import type { DeployableFile } from '@/types/file'

vi.mock('@/api/files', () => ({
  filesApi: {
    list:       vi.fn(),
    upload:     vi.fn(),
    delete:     vi.fn(),
    deployFile: vi.fn(),
  },
}))

import { filesApi } from '@/api/files'

const FILE: DeployableFile = {
  id: 'f1', tenant_id: 't1', name: 'installer.msi', size_bytes: 1024,
  checksum_sha256: 'abcd', created_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useFiles', () => {
  it('retourne la liste des fichiers', async () => {
    vi.mocked(filesApi.list).mockResolvedValue({ data: [FILE] })

    const { result } = renderHook(() => useFiles(), { wrapper: createWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.data).toEqual([FILE])
  })
})

describe('useUploadFile', () => {
  it('appelle filesApi.upload avec le fichier et le nom fournis', async () => {
    vi.mocked(filesApi.upload).mockResolvedValue({ data: FILE })
    const file = new File(['contenu'], 'installer.msi')

    const { result } = renderHook(() => useUploadFile(), { wrapper: createWrapper() })
    result.current.mutate({ file, name: 'installer.msi' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(filesApi.upload).toHaveBeenCalledWith(file, 'installer.msi')
  })

  it("expose l'erreur si un fichier du même nom existe déjà", async () => {
    vi.mocked(filesApi.upload).mockRejectedValue(new Error('un fichier avec ce nom existe déjà'))
    const file = new File(['x'], 'dup.bin')

    const { result } = renderHook(() => useUploadFile(), { wrapper: createWrapper() })
    result.current.mutate({ file })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toBe('un fichier avec ce nom existe déjà')
  })
})

describe('useDeleteFile', () => {
  it('appelle filesApi.delete avec l\'ID', async () => {
    vi.mocked(filesApi.delete).mockResolvedValue(undefined)

    const { result } = renderHook(() => useDeleteFile(), { wrapper: createWrapper() })
    result.current.mutate('f1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(filesApi.delete).toHaveBeenCalledWith('f1')
  })
})

describe('useDeployFile', () => {
  it('déploie le fichier choisi sur l\'agent donné', async () => {
    vi.mocked(filesApi.deployFile).mockResolvedValue({ data: { command_id: 'c1', status: 'pending', sent: true } })

    const { result } = renderHook(() => useDeployFile('a1'), { wrapper: createWrapper() })
    result.current.mutate({ file_id: 'f1' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(filesApi.deployFile).toHaveBeenCalledWith('a1', { file_id: 'f1' })
    expect(result.current.data?.data.command_id).toBe('c1')
  })
})
