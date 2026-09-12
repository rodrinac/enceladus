package jobstatus

import "testing"

func TestLifecycle(t *testing.T) {
	registry := NewRegistry()
	registry.Register("abc-123", "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL",
		[]string{"CE", "BA"}, "2020", "2022")

	jobs := registry.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.IDRequisicao != "abc-123" || job.Status != StatusQueued {
		t.Fatalf("unexpected initial job: %+v", job)
	}
	if job.Estado != "CE · BA" {
		t.Fatalf("estado join wrong: %q", job.Estado)
	}
	if job.CriadoEm == "" {
		t.Fatal("missing criado_em")
	}

	registry.MarkRunning("abc-123")
	job = registry.List()[0]
	if job.Status != StatusRunning {
		t.Fatalf("expected running, got %s", job.Status)
	}

	registry.MarkFailed("abc-123")
	job = registry.List()[0]
	if job.Status != StatusFailed || job.Mensagem == nil || *job.Mensagem != "Não foi possível gerar este relatório." {
		t.Fatalf("unexpected failed job: %+v", job)
	}

	registry.MarkSucceeded("abc-123")
	if len(registry.List()) != 0 {
		t.Fatalf("succeeded job should be removed: %+v", registry.List())
	}
}

func TestMarkUnknownIsNoop(t *testing.T) {
	registry := NewRegistry()
	registry.MarkRunning("missing")
	registry.MarkFailed("missing")
	registry.MarkSucceeded("missing")
	if len(registry.List()) != 0 {
		t.Fatalf("expected empty registry: %+v", registry.List())
	}
}
