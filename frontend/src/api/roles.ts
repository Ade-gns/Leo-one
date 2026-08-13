import { get } from './client'
import type { ApiResponse } from '@/types/api'
import type { Role } from '@/types/user'

const BASE = '/api/v1/roles'

export const rolesApi = {
  // Lecture seule pour l'instant — la gestion des rôles personnalisés
  // (création/modification/suppression) n'est pas encore implémentée côté
  // backend (RoleHandler.Create/Update/Delete renvoient 501).
  list: () =>
    get<ApiResponse<Role[]>>(BASE),
}
