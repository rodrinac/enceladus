import importlib.util
from pathlib import Path

SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "probe_dependencies.py"
SPEC = importlib.util.spec_from_file_location("probe_dependencies", SCRIPT_PATH)
assert SPEC and SPEC.loader
probe_dependencies = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe_dependencies)


def test_datasus_years_accepts_case_and_ignores_unrelated_files() -> None:
    assert probe_dependencies.datasus_years([
        "DOAC2023.dbc", "DODF2024.DBC", "DODF2024.dbc.part", "readme.txt"
    ]) == [2023, 2024]


def test_latest_datasus_file_selects_latest_year_for_state() -> None:
    assert probe_dependencies.latest_datasus_file([
        "DOSP2024.dbc", "DODF2023.DBC", "dodf2024.dbc"
    ]) == "dodf2024.dbc"


def test_timed_probe_captures_failure() -> None:
    def fail() -> str:
        raise TimeoutError("offline")

    result = probe_dependencies.timed_probe("example", fail)

    assert result.ok is False
    assert result.name == "example"
    assert result.detail == "TimeoutError: offline"
