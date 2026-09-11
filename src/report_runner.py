"""Shared execution boundary for the R report scripts."""

from pathlib import Path
from subprocess import PIPE, STDOUT, CompletedProcess, TimeoutExpired, run
from typing import Sequence

DEFAULT_TIMEOUT_SECONDS = 900


def run_rscript(script: Path, arguments: Sequence[object], *, cwd: Path, timeout: int = DEFAULT_TIMEOUT_SECONDS) -> CompletedProcess[str]:
    """Run an R script with consistent output capture and timeout handling."""
    command = ["Rscript", str(script), *(str(argument) for argument in arguments)]
    try:
        return run(command, check=False, cwd=cwd, stdout=PIPE, stderr=STDOUT, text=True, timeout=timeout)
    except TimeoutExpired as error:
        output = error.stdout or ""
        if isinstance(output, bytes):
            output = output.decode(errors="replace")
        return CompletedProcess(command, 124, stdout=f"{output}\nRscript excedeu o timeout de {timeout}s.")
