import importlib
from pathlib import Path
from unittest.mock import Mock

import pytest

import job_status


def setup_function():
    job_status._jobs.clear()


@pytest.fixture
def load_main(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENCELADUS_HOME", str(tmp_path))
    return importlib.import_module("main")


def test_successful_worker_removes_pending_job(load_main):
    job_status.register("request-1", "Densidade", ["DF"], "2024-01-01", "2024-12-31")
    worker = Mock(return_value=True)

    load_main._processar_relatorio("request-1", worker, "argument")

    worker.assert_called_once_with("argument", "request-1")
    assert job_status.list_jobs() == []


def test_failed_worker_marks_job_as_failed(load_main):
    job_status.register("request-2", "Densidade", ["DF"], "2024-01-01", "2024-12-31")

    load_main._processar_relatorio("request-2", Mock(return_value=False))

    assert job_status.list_jobs()[0]["status"] == "failed"