import csv
import importlib.util
import io
import json
import zipfile
from pathlib import Path

SCRIPT_PATH = Path(__file__).parents[1] / "src" / "scripts" / "fetch_sim_archives.py"
SPEC = importlib.util.spec_from_file_location("fetch_sim_archives", SCRIPT_PATH)
assert SPEC and SPEC.loader
fetch_sim_archives = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fetch_sim_archives)


def test_filter_year_keeps_only_selected_state(tmp_path: Path) -> None:
    source = tmp_path / "sim.csv"
    source.write_text(
        '"CODMUNRES";"CAUSABAS"\n"530010";"X00"\n"355030";"W85"\n',
        encoding="utf-8",
    )
    output = io.StringIO()

    count = fetch_sim_archives.filter_year(source, output, {"53"}, True)

    assert count == 1
    assert list(csv.DictReader(io.StringIO(output.getvalue()), delimiter=";")) == [
        {"CODMUNRES": "530010", "CAUSABAS": "X00"}
    ]


def test_all_supported_years_have_https_urls() -> None:
    assert set(fetch_sim_archives.ARCHIVE_URLS) == set(range(2019, 2025))
    assert all(url.startswith("https://") for url in fetch_sim_archives.ARCHIVE_URLS.values())


def test_filter_year_streams_json_zip(tmp_path: Path) -> None:
    source = tmp_path / "sim.zip"
    with zipfile.ZipFile(source, "w") as archive:
        archive.writestr(
            "part-1.json",
            json.dumps([
                {"CODMUNRES": "530010", "CAUSABAS": "X00"},
                {"CODMUNRES": "355030", "CAUSABAS": "W85"},
            ]),
        )
    output = io.StringIO()

    count = fetch_sim_archives.filter_year(source, output, {"53"}, True)

    assert count == 1
    assert list(csv.DictReader(io.StringIO(output.getvalue()), delimiter=";")) == [
        {"CODMUNRES": "530010", "CAUSABAS": "X00"}
    ]
