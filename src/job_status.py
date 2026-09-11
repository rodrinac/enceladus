"""In-memory status registry for report requests.

The registry intentionally does not survive an application restart. It is a
small, UI-facing step before report execution moves to a durable queue.
"""

from datetime import datetime
from threading import Lock
from typing import Literal, TypedDict

Status = Literal["queued", "running", "failed"]


class Job(TypedDict):
    criado_em: str
    data_fim: str
    data_inicio: str
    estado: str
    id_requisicao: str
    mensagem: str | None
    status: Status
    tipo: str


_jobs: dict[str, Job] = {}
_lock = Lock()


def register(
    request_id: str,
    report_type: str,
    states: list[str],
    start_date: str,
    end_date: str,
) -> None:
    with _lock:
        _jobs[request_id] = Job(
            criado_em=datetime.now().isoformat(timespec="seconds"),
            data_fim=end_date,
            data_inicio=start_date,
            estado=" · ".join(states),
            id_requisicao=request_id,
            mensagem=None,
            status="queued",
            tipo=report_type,
        )


def mark_running(request_id: str) -> None:
    with _lock:
        if request_id in _jobs:
            _jobs[request_id]["status"] = "running"


def mark_failed(request_id: str) -> None:
    with _lock:
        if request_id in _jobs:
            _jobs[request_id]["status"] = "failed"
            _jobs[request_id]["mensagem"] = "Não foi possível gerar este relatório."


def mark_succeeded(request_id: str) -> None:
    with _lock:
        _jobs.pop(request_id, None)


def list_jobs() -> list[Job]:
    with _lock:
        return list(_jobs.values())
