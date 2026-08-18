package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_Allow_BlocksAfterMax(t *testing.T) {
	l := New(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("tentative %d aurait dû être autorisée", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("la 4e tentative aurait dû être bloquée")
	}
}

func TestLimiter_Allow_IndependentKeys(t *testing.T) {
	l := New(1, time.Minute)

	if !l.Allow("1.2.3.4") {
		t.Fatal("première tentative pour 1.2.3.4 aurait dû être autorisée")
	}
	if !l.Allow("5.6.7.8") {
		t.Fatal("une autre clé ne doit pas être affectée par la limite de 1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("1.2.3.4 aurait dû rester bloquée")
	}
}

func TestLimiter_Allow_WindowExpires(t *testing.T) {
	l := New(1, 20*time.Millisecond)

	if !l.Allow("k") {
		t.Fatal("première tentative aurait dû être autorisée")
	}
	if l.Allow("k") {
		t.Fatal("deuxième tentative immédiate aurait dû être bloquée")
	}

	time.Sleep(30 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("après expiration de la fenêtre, la tentative aurait dû être autorisée")
	}
}

func TestLimiter_Blocked_DoesNotRecord(t *testing.T) {
	l := New(1, time.Minute)

	if l.Blocked("k") {
		t.Fatal("clé jamais vue ne devrait pas être bloquée")
	}
	if l.Blocked("k") {
		t.Fatal("Blocked ne doit pas enregistrer de tentative — appelé deux fois, toujours pas bloqué")
	}

	l.RecordFailure("k")
	if !l.Blocked("k") {
		t.Fatal("après 1 échec (max=1), la clé devrait être bloquée")
	}
}

func TestLimiter_Reset(t *testing.T) {
	l := New(1, time.Minute)

	l.RecordFailure("k")
	if !l.Blocked("k") {
		t.Fatal("devrait être bloquée avant reset")
	}

	l.Reset("k")
	if l.Blocked("k") {
		t.Fatal("ne devrait plus être bloquée après reset")
	}
}
