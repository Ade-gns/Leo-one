/** Enveloppe de réponse API standard */
export interface ApiResponse<T> {
  data: T
  meta?: PaginationMeta
}

/** Pagination cursor-based */
export interface PaginationMeta {
  cursor?:   string
  total?:    number
  limit:     number
  has_more:  boolean
}

/** Format d'erreur standard de l'API */
export interface ApiError {
  error: {
    code:     string
    message:  string
    details?: Record<string, string[]>
  }
}

/** Paramètres de pagination pour les requêtes */
export interface PaginationParams {
  cursor?: string
  limit?:  number
}
