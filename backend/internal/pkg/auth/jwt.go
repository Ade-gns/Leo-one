// Package auth fournit les utilitaires d'authentification JWT (HS256).
package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier gère la création et la vérification des tokens JWT (HS256).
type JWTVerifier struct {
	secret []byte
}

// NewJWTVerifier crée un JWTVerifier avec le secret HMAC donné.
func NewJWTVerifier(secret string) *JWTVerifier {
	return &JWTVerifier{secret: []byte(secret)}
}

// Sign crée un token JWT signé avec les claims fournis.
func (v *JWTVerifier) Sign(claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(v.secret)
}

// Verify valide un token JWT et retourne ses claims.
func (v *JWTVerifier) Verify(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("méthode de signature inattendue: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("token invalide")
	}
	return claims, nil
}
