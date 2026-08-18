import { get, postForm, post, del } from './client'
import type { ApiResponse } from '@/types/api'
import type { DeployableFile, DeployFilePayload } from '@/types/file'

const FILES_BASE  = '/api/v1/files'
const AGENTS_BASE  = '/api/v1/agents'

export const filesApi = {
  list: () =>
    get<ApiResponse<DeployableFile[]>>(FILES_BASE),

  /** Uploade un fichier — name optionnel (défaut : nom du fichier local). */
  upload: (file: globalThis.File, name?: string) => {
    const form = new FormData()
    form.append('file', file)
    if (name) form.append('name', name)
    return postForm<ApiResponse<DeployableFile>>(FILES_BASE, form)
  },

  delete: (fileID: string) =>
    del<void>(`${FILES_BASE}/${fileID}`),

  deployFile: (agentID: string, payload: DeployFilePayload) =>
    post<ApiResponse<{ command_id: string; status: string; sent: boolean }>>(
      `${AGENTS_BASE}/${agentID}/deploy-file`, payload,
    ),
}
