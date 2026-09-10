#!/usr/bin/env python3
"""Download official OpenDataSUS SIM archives and retain selected states."""

import argparse
import csv
import io
import json
import os
import shutil
import sys
import tempfile
import urllib.request
import zipfile
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator, TextIO

ARCHIVE_URLS = {
    2019: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2019_csv.zip",
    2020: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2020_csv.zip",
    2021: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2021_csv.zip",
    2022: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/json/Mortalidade_Geral_2022_json.zip",
    2023: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2023_csv.zip",
    2024: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/DO24OPEN_csv.zip",
}
STATE_CODES = {
    "RO": "11", "AC": "12", "AM": "13", "RR": "14", "PA": "15", "AP": "16",
    "TO": "17", "MA": "21", "PI": "22", "CE": "23", "RN": "24", "PB": "25",
    "PE": "26", "AL": "27", "SE": "28", "BA": "29", "MG": "31", "ES": "32",
    "RJ": "33", "SP": "35", "PR": "41", "SC": "42", "RS": "43", "MS": "50",
    "MT": "51", "GO": "52", "DF": "53",
}
USER_AGENT = "enceladus-open-datasus/1.0"


def download(url: str, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        dir=destination.parent, prefix=f".{destination.name}.", suffix=".part"
    )
    os.close(descriptor)
    temporary_path = Path(temporary_name)
    try:
        request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
        with urllib.request.urlopen(request, timeout=120) as response:
            with temporary_path.open("wb") as output:
                shutil.copyfileobj(response, output, length=1024 * 1024)
        temporary_path.replace(destination)
    finally:
        temporary_path.unlink(missing_ok=True)


def cached_source(year: int, cache_directory: Path) -> Path:
    url = ARCHIVE_URLS[year]
    suffix = ".zip" if url.endswith(".zip") else ".csv"
    destination = cache_directory / "archives" / f"sim-{year}{suffix}"
    if not destination.exists() or destination.stat().st_size == 0:
        download(url, destination)
    return destination


@contextmanager
def open_csv(source: Path) -> Iterator[TextIO]:
    if source.suffix == ".zip":
        with zipfile.ZipFile(source) as archive:
            members = [name for name in archive.namelist() if name.lower().endswith(".csv")]
            if len(members) != 1:
                raise ValueError(f"expected one CSV in {source.name}, found {len(members)}")
            with archive.open(members[0]) as binary:
                with io.TextIOWrapper(binary, encoding="utf-8-sig", newline="") as text:
                    yield text
    else:
        with source.open(encoding="utf-8-sig", newline="") as text:
            yield text


def iter_json_array(input_file: TextIO) -> Iterator[dict[str, object]]:
    decoder = json.JSONDecoder()
    buffer = ""
    array_started = False
    finished = False
    while not finished:
        chunk = input_file.read(1024 * 1024)
        if chunk:
            buffer += chunk
        else:
            finished = True
        while True:
            buffer = buffer.lstrip()
            if not array_started:
                if not buffer:
                    break
                if not buffer.startswith("["):
                    raise ValueError("JSON resource is not an array")
                buffer = buffer[1:]
                array_started = True
                continue
            buffer = buffer.lstrip()
            if buffer.startswith(","):
                buffer = buffer[1:]
                continue
            if buffer.startswith("]"):
                return
            if not buffer:
                break
            try:
                record, end = decoder.raw_decode(buffer)
            except json.JSONDecodeError:
                if finished:
                    raise
                break
            if not isinstance(record, dict):
                raise ValueError("JSON resource contains a non-object record")
            yield record
            buffer = buffer[end:]
    raise ValueError("JSON resource ended before the array was closed")


def iter_records(source: Path) -> Iterator[dict[str, object]]:
    if source.suffix != ".zip":
        with open_csv(source) as input_file:
            yield from csv.DictReader(input_file, delimiter=";")
        return

    with zipfile.ZipFile(source) as archive:
        csv_members = [name for name in archive.namelist() if name.lower().endswith(".csv")]
        json_members = [name for name in archive.namelist() if name.lower().endswith(".json")]
        if csv_members:
            if len(csv_members) != 1:
                raise ValueError(f"expected one CSV in {source.name}, found {len(csv_members)}")
            with archive.open(csv_members[0]) as binary:
                with io.TextIOWrapper(binary, encoding="utf-8-sig", newline="") as text:
                    yield from csv.DictReader(text, delimiter=";")
            return
        if json_members:
            for member in sorted(json_members):
                with archive.open(member) as binary:
                    with io.TextIOWrapper(binary, encoding="utf-8-sig") as text:
                        yield from iter_json_array(text)
            return
    raise ValueError(f"{source.name} contains no supported CSV or JSON resources")


def source_fieldnames(source: Path) -> list[str]:
    first_record = next(iter_records(source), None)
    fieldnames = list(first_record) if first_record else []
    if "CODMUNRES" not in fieldnames:
        raise ValueError(f"{source.name} has no CODMUNRES column")
    return fieldnames


def filter_year(
    source: Path,
    output: TextIO,
    state_codes: set[str],
    write_header: bool,
    output_fieldnames: list[str] | None = None,
) -> int:
    fieldnames = output_fieldnames or source_fieldnames(source)
    writer = csv.DictWriter(output, fieldnames=fieldnames, delimiter=";", lineterminator="\n")
    if write_header:
        writer.writeheader()
    count = 0
    for row in iter_records(source):
        municipality = row.get("CODMUNRES")
        if isinstance(municipality, str) and municipality[:2] in state_codes:
            writer.writerow({field: row.get(field) for field in fieldnames})
            count += 1
    return count


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--year-start", type=int, required=True)
    parser.add_argument("--year-end", type=int, required=True)
    parser.add_argument("--states", required=True, help="comma-separated state abbreviations")
    parser.add_argument("--cache-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    years = list(range(args.year_start, args.year_end + 1))
    unsupported = [year for year in years if year not in ARCHIVE_URLS]
    if unsupported:
        raise ValueError(f"no official SIM archive configured for years: {unsupported}")
    states = {state.strip().upper() for state in args.states.split(",") if state.strip()}
    unknown_states = states.difference(STATE_CODES)
    if unknown_states:
        raise ValueError(f"unknown state abbreviations: {sorted(unknown_states)}")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    sources = [cached_source(year, args.cache_dir) for year in years]
    fieldnames = list(dict.fromkeys(
        field for source in sources for field in source_fieldnames(source)
    ))
    row_count = 0
    with args.output.open("w", encoding="utf-8", newline="") as output:
        for index, source in enumerate(sources):
            row_count += filter_year(
                source,
                output,
                {STATE_CODES[state] for state in states},
                index == 0,
                fieldnames,
            )
    print(f"Extracted {row_count} SIM records for {','.join(sorted(states))}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
