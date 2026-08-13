import { get, post, del } from './client'
import type { ApiResponse } from '@/types/api'
import type {
  EnrollmentToken, EnrollmentTokenCreateResponse, CreateEnrollmentTokenPayload,
} from '@/types/enrollmentToken'

const BASE = '/api/v1/enrollment-tokens'

export const enrollmentTokensApi = {
  list: () =>
    get<ApiResponse<EnrollmentToken[]>>(BASE),

  create: (payload: CreateEnrollmentTokenPayload) =>
    post<ApiResponse<EnrollmentTokenCreateResponse>>(BASE, payload),

  delete: (tokenID: string) =>
    del<void>(`${BASE}/${tokenID}`),
}
