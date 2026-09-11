from pathlib import Path
from subprocess import CompletedProcess, TimeoutExpired

import report_runner


def test_run_rscript_builds_command_and_captures_output(monkeypatch, tmp_path):
    captured = {}

    def fake_run(command, **kwargs):
        captured.update(command=command, kwargs=kwargs)
        return CompletedProcess(command, 0, stdout="ok")

    monkeypatch.setattr(report_runner, "run", fake_run)

    result = report_runner.run_rscript(Path("script.R"), ["DF", 2024], cwd=tmp_path)

    assert result.returncode == 0
    assert captured["command"] == ["Rscript", "script.R", "DF", "2024"]
    assert captured["kwargs"]["timeout"] == 900
    assert captured["kwargs"]["text"] is True
    assert captured["kwargs"]["stderr"] == report_runner.STDOUT


def test_run_rscript_converts_timeout_to_failed_process(monkeypatch, tmp_path):
    def fake_run(command, **kwargs):
        raise TimeoutExpired(command, kwargs["timeout"], output=b"partial output")

    monkeypatch.setattr(report_runner, "run", fake_run)

    result = report_runner.run_rscript(Path("script.R"), [], cwd=tmp_path, timeout=12)

    assert result.returncode == 124
    assert "partial output" in result.stdout
    assert "12s" in result.stdout
