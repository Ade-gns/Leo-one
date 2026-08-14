import { get, post, patch, del } from './client'
import type { ApiResponse } from '@/types/api'
import type { Role, Permission } from '@/types/user'

const ROLES_BASE = '/api/v1/roles'
const PERMISSIONS_BASE = '/api/v1/permissions'

export interface CreateRolePayload {
  name:            string
  description?:    string
  permission_ids?: string[]
}

export interface UpdateRolePayload {
  name?:           string
  description?:    string
  permission_ids?: string[]
}

export const rolesApi = {
  list: () =>
    get<ApiResponse<Role[]>>(ROLES_BASE),

  listPermissions: () =>
    get<ApiResponse<Permission[]>>(PERMISSIONS_BASE),

  create: (payload: CreateRolePayload) =>
    post<ApiResponse<Role>>(ROLES_BASE, payload),

  update: (roleID: string, payload: UpdateRolePayload) =>
    patch<ApiResponse<Role>>(`${ROLES_BASE}/${roleID}`, payload),

  // Rejeté par le backend (403 SYSTEM_ROLE_IMMUTABLE) si le rôle est
  // système (is_system=true) — voir RoleHandler.Delete.
  delete: (roleID: string) =>
    del<void>(`${ROLES_BASE}/${roleID}`),
}
