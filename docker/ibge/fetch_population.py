import csv
import ftplib
import json
import os
import re
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

SIDRA_URL = (
    "https://apisidra.ibge.gov.br/values/t/6579/n6/all/v/9324/"
    "p/{period}?formato=json"
)
OUTPUT_PATH = Path("/data/population.csv")
DATASUS_MAX_YEAR_PATH = Path("/data/datasus-max-year.txt")
DATASUS_HOST = "ftp.datasus.gov.br"
DATASUS_DIRECTORY = "/dissemin/publicos/SIM/CID10/DORES"
EXPECTED_FIELDS = {"D1C", "D1N", "V", "D3C"}
MINIMUM_MUNICIPALITIES = 5_500


def discover_datasus_max_year(timeout: int = 20) -> int:
    with ftplib.FTP(DATASUS_HOST, timeout=timeout) as ftp:
        ftp.login()
        ftp.cwd(DATASUS_DIRECTORY)
        file_names = ftp.nlst()

    years = [
        int(match.group(1))
        for file_name in file_names
        if (match := re.fullmatch(r"DO[A-Z]{2}(\d{4})\.DBC", Path(file_name).name, re.IGNORECASE))
    ]
    if not years:
        raise ValueError("DataSUS returned no final SIM-DO files")
    return max(years)


def write_text_atomically(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(dir=path.parent, prefix=f"{path.stem}-")
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            output.write(value)
        temporary_path.chmod(0o644)
        temporary_path.replace(path)
    except BaseException:
        temporary_path.unlink(missing_ok=True)
        raise


def refresh_datasus_max_year() -> None:
    fallback = int(os.getenv("DATASUS_MAX_YEAR_FALLBACK", "2024"))
    try:
        max_year = discover_datasus_max_year()
        write_text_atomically(DATASUS_MAX_YEAR_PATH, f"{max_year}\n")
        print(f"Discovered DataSUS SIM-DO data through {max_year}")
    except (OSError, ValueError, ftplib.Error) as error:
        if DATASUS_MAX_YEAR_PATH.exists():
            print(f"DataSUS discovery unavailable; retaining cached maximum year: {error}")
        else:
            write_text_atomically(DATASUS_MAX_YEAR_PATH, f"{fallback}\n")
            print(f"DataSUS discovery unavailable; using fallback year {fallback}: {error}")


def fetch_records(period: str) -> list[dict[str, str]]:
    request = urllib.request.Request(
        SIDRA_URL.format(period=period),
        headers={"User-Agent": "enceladus-population-fetcher/1.0"},
    )
    with urllib.request.urlopen(request, timeout=60) as response:
        payload = json.load(response)

    if not isinstance(payload, list) or len(payload) < 2:
        raise ValueError("SIDRA returned no municipal population records")

    records = payload[1:]
    if not EXPECTED_FIELDS.issubset(records[0]):
        raise ValueError("SIDRA response does not contain the expected fields")

    return records


def normalize(records: list[dict[str, str]]) -> list[dict[str, str | int]]:
    municipalities: list[dict[str, str | int]] = []
    seen_codes: set[str] = set()

    for record in records:
        city_code = record["D1C"]
        population = record["V"].replace(" ", "")
        city_and_state = record["D1N"].rsplit(" - ", maxsplit=1)

        if (
            len(city_and_state) != 2
            or len(city_code) != 7
            or not city_code.isdigit()
            or not population.isdigit()
            or int(population) <= 0
            or city_code in seen_codes
        ):
            raise ValueError(f"Invalid SIDRA municipality record: {record!r}")

        city, state = city_and_state
        seen_codes.add(city_code)
        municipalities.append(
            {
                "state": state,
                "state_ibge_code": city_code[:2],
                "city_ibge_code": city_code,
                "city": city,
                "estimated_population": int(population),
                "reference_year": record["D3C"],
            }
        )

    if len(municipalities) < MINIMUM_MUNICIPALITIES:
        raise ValueError(
            f"SIDRA returned only {len(municipalities)} municipalities; refusing partial data"
        )

    return municipalities


def write_atomically(rows: list[dict[str, str | int]]) -> None:
    OUTPUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        dir=OUTPUT_PATH.parent,
        prefix="population-",
        suffix=".csv",
        text=True,
    )
    temporary_path = Path(temporary_name)

    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="") as output:
            writer = csv.DictWriter(output, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)
        temporary_path.chmod(0o644)
        temporary_path.replace(OUTPUT_PATH)
    except BaseException:
        temporary_path.unlink(missing_ok=True)
        raise


def has_valid_cache() -> bool:
    try:
        with OUTPUT_PATH.open(encoding="utf-8", newline="") as source:
            rows = list(csv.DictReader(source))
        required_columns = {
            "state",
            "state_ibge_code",
            "city_ibge_code",
            "city",
            "estimated_population",
            "reference_year",
        }
        return (
            len(rows) >= MINIMUM_MUNICIPALITIES
            and required_columns.issubset(rows[0])
            and len({row["city_ibge_code"] for row in rows}) == len(rows)
            and all(
                len(row["city_ibge_code"]) == 7
                and row["city_ibge_code"].isdigit()
                and row["estimated_population"].isdigit()
                and int(row["estimated_population"]) > 0
                and len(row["state"]) == 2
                for row in rows
            )
        )
    except (OSError, IndexError):
        return False


def main() -> None:
    period = os.getenv("IBGE_POPULATION_PERIOD", "last%201")
    last_error: Exception | None = None

    for attempt in range(1, 4):
        try:
            rows = normalize(fetch_records(period))
            write_atomically(rows)
            print(
                f"Saved {len(rows)} IBGE municipality estimates for "
                f"{rows[0]['reference_year']} to {OUTPUT_PATH}"
            )
            refresh_datasus_max_year()
            return
        except (OSError, ValueError, json.JSONDecodeError, urllib.error.URLError) as error:
            last_error = error
            if attempt < 3:
                time.sleep(attempt * 2)

    if has_valid_cache():
        print(f"IBGE is unavailable; retaining cached population data: {last_error}")
        refresh_datasus_max_year()
        return

    raise RuntimeError("Unable to fetch IBGE population data and no cache exists") from last_error


if __name__ == "__main__":
    main()
