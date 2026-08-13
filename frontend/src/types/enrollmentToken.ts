/**
 * enrollmentToken.ts — Types pour les tokens d'enrôlement d'agents
 */

/** Token d'enrôlement tel que renvoyé par GET /api/v1/enrollment-tokens
 *  (jamais le token brut — seul son hash est conservé côté backend). */
export interface EnrollmentToken {
  id:          string
  label?:      string
  expires_at:  string
  used_at?:    string
  created_at:  string
}

/** Réponse de POST /api/v1/enrollment-tokens — le token brut n'est renvoyé
 *  qu'une seule fois, ici, jamais retrouvable ensuite. */
export interface EnrollmentTokenCreateResponse {
  id:          string
  token:       string
  expires_at:  string
}

export interface CreateEnrollmentTokenPayload {
  label?:            string
  expires_in_hours?: number
}
