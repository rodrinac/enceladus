import importlib.util
from pathlib import Path

import pytest

SCRIPT_PATH = Path(__file__).parents[1] / "docker" / "ibge" / "fetch_population.py"
SPEC = importlib.util.spec_from_file_location("fetch_population", SCRIPT_PATH)
assert SPEC and SPEC.loader
fetch_population = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fetch_population)


def test_normalize_maps_sidra_fields(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(fetch_population, "MINIMUM_MUNICIPALITIES", 1)

    rows = fetch_population.normalize(
        [{"D1C": "1100015", "D1N": "Alta Floresta D'Oeste - RO", "V": "22724", "D3C": "2026"}]
    )

    assert rows == [
        {
            "state": "RO",
            "state_ibge_code": "11",
            "city_ibge_code": "1100015",
            "city": "Alta Floresta D'Oeste",
            "estimated_population": 22724,
            "reference_year": "2026",
        }
    ]


def test_normalize_rejects_invalid_population(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(fetch_population, "MINIMUM_MUNICIPALITIES", 1)

    with pytest.raises(ValueError, match="Invalid SIDRA municipality"):
        fetch_population.normalize(
            [{"D1C": "1100015", "D1N": "Example - RO", "V": "-", "D3C": "2026"}]
        )


def test_invalid_cache_is_rejected(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    cache_path = tmp_path / "population.csv"
    cache_path.write_text("city,estimated_population\nExample,100\n", encoding="utf-8")
    monkeypatch.setattr(fetch_population, "OUTPUT_PATH", cache_path)

    assert fetch_population.has_valid_cache() is False
