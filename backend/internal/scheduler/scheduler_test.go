package scheduler

import (
	"testing"
	"time"
)

func TestComputeNextRun(t *testing.T) {
	// Mardi 2026-08-18 10:00:00 UTC (choisi arbitrairement, milieu de semaine
	// pour que le calcul "hebdomadaire" ci-dessous ne tombe pas le jour même).
	from := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		expression string
		want       time.Time
	}{
		{
			name:       "toutes les heures",
			expression: "0 * * * *",
			want:       time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC),
		},
		{
			name:       "tous les jours à 02:00 (lendemain, l'heure du jour est déjà passée)",
			expression: "0 2 * * *",
			want:       time.Date(2026, 8, 19, 2, 0, 0, 0, time.UTC),
		},
		{
			name:       "chaque lundi à 09:30",
			expression: "30 9 * * 1",
			want:       time.Date(2026, 8, 24, 9, 30, 0, 0, time.UTC), // prochain lundi
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeNextRun(tt.expression, from)
			if err != nil {
				t.Fatalf("computeNextRun(%q) a échoué : %v", tt.expression, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("computeNextRun(%q) = %v, attendu %v", tt.expression, got, tt.want)
			}
		})
	}
}

func TestComputeNextRun_InvalidExpression(t *testing.T) {
	if _, err := computeNextRun("pas une expression cron", time.Now()); err == nil {
		t.Error("attendu une erreur pour une expression cron invalide, obtenu nil")
	}
}
