#!/usr/bin/env python3
"""Download official OpenDataSUS SIM archives and retain selected states."""

import argparse
import csv
import io
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
    2019: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/Mortalidade_Geral_2019_csv.zip",
    2020: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/Mortalidade_Geral_2020_csv.zip",
    2021: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/Mortalidade_Geral_2021_csv.zip",
    2022: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/DO22OPEN.csv",
    2023: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/DO23OPEN.csv",
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


def csv_fieldnames(source: Path) -> list[str]:
    with open_csv(source) as input_file:
        fieldnames = csv.DictReader(input_file, delimiter=";").fieldnames
    if not fieldnames or "CODMUNRES" not in fieldnames:
        raise ValueError(f"{source.name} has no CODMUNRES column")
    return fieldnames


def filter_year(
    source: Path,
    output: TextIO,
    state_codes: set[str],
    write_header: bool,
    output_fieldnames: list[str] | None = None,
) -> int:
    with open_csv(source) as input_file:
        reader = csv.DictReader(input_file, delimiter=";")
        if not reader.fieldnames or "CODMUNRES" not in reader.fieldnames:
            raise ValueError(f"{source.name} has no CODMUNRES column")
        writer = csv.DictWriter(
            output,
            fieldnames=output_fieldnames or reader.fieldnames,
            delimiter=";",
            lineterminator="\n",
        )
        if write_header:
            writer.writeheader()
        count = 0
        for row in reader:
            if row.get("CODMUNRES", "")[:2] in state_codes:
                writer.writerow(row)
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
        field for source in sources for field in csv_fieldnames(source)
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
