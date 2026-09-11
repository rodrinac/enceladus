import job_status


def setup_function():
    job_status._jobs.clear()


def test_job_lifecycle_keeps_pending_and_failed_jobs_visible():
    job_status.register("request-1", "Densidade", ["DF"], "2024-01-01", "2024-12-31")

    assert job_status.list_jobs()[0]["status"] == "queued"

    job_status.mark_running("request-1")
    assert job_status.list_jobs()[0]["status"] == "running"

    job_status.mark_failed("request-1")
    job = job_status.list_jobs()[0]
    assert job["status"] == "failed"
    assert job["mensagem"] == "Não foi possível gerar este relatório."


def test_completed_job_is_removed_so_pdf_listing_is_the_source_of_truth():
    job_status.register("request-2", "Densidade", ["DF"], "2024-01-01", "2024-12-31")

    job_status.mark_succeeded("request-2")

    assert job_status.list_jobs() == []
