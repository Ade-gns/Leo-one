import { get, post, patch, del } from './client'
import type { ApiResponse } from '@/types/api'
import type { User } from '@/types/user'

const BASE = '/api/v1/users'

export interface CreateUserPayload {
  email:      string
  full_name:  string
  password:   string
  role_ids?:  string[]
}

export interface UpdateUserPayload {
  full_name?: string
  is_active?: boolean
  role_ids?:  string[]
}

export const usersApi = {
  list: () =>
    get<ApiResponse<User[]>>(BASE),

  get: (userID: string) =>
    get<ApiResponse<User>>(`${BASE}/${userID}`),

  create: (payload: CreateUserPayload) =>
    post<ApiResponse<User>>(BASE, payload),

  update: (userID: string, payload: UpdateUserPayload) =>
    patch<ApiResponse<User>>(`${BASE}/${userID}`, payload),

  delete: (userID: string) =>
    del<void>(`${BASE}/${userID}`),
}
