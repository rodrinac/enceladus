package reportstore

import (
	"context"
	"testing"
)

func TestCleanKey(t *testing.T) {
	for _, key := range []string{
		"queimaduras/densidade-municipal-por-periodo-geral/AC-CE.2022-01-01.2022-12-31.pdf",
		"queimaduras/casos-mensais-por-municipio-por-estado/AC.2021.2021.pdf",
	} {
		if _, err := cleanKey(key); err != nil {
			t.Fatalf("expected %q to be valid: %v", key, err)
		}
	}
	for _, key := range []string{
		"",
		"/absoluta.pdf",
		"queimaduras/../../etc/passwd",
		"queimaduras/./rel.pdf",
		"queimaduras//dupla.pdf",
		"queimaduras/com espaço.pdf",
		`queimaduras\windows.pdf`,
		"CE.2022-01-01.2022-12-31.pdf;rm -rf",
	} {
		if _, err := cleanKey(key); err == nil {
			t.Fatalf("expected %q to be rejected", key)
		}
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	local := &Local{Dir: t.TempDir()}
	ctx := context.Background()
	if err := local.Put(ctx, "ok/CE.2021.2021.pdf", []byte("%PDF"), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Get(ctx, "../fora.pdf"); err == nil {
		t.Fatal("expected traversal read to fail")
	}
	if _, err := local.Get(ctx, "ok/CE.2021.2021.pdf"); err != nil {
		t.Fatalf("expected valid read to succeed: %v", err)
	}
}
