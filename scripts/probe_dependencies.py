#!/usr/bin/env python3
"""Probe the external services used by Enceladus without changing remote state."""

import argparse
import ftplib
import gzip
import json
import os
import re
import socket
import ssl
import sys
import time
import urllib.request
from dataclasses import asdict, dataclass
from typing import Callable

SIDRA_URL = (
    "https://apisidra.ibge.gov.br/values/t/6579/n6/5300108/v/9324/"
    "p/last%201?formato=json"
)
IBGE_PERIODS_URL = "https://servicodados.ibge.gov.br/api/v3/agregados/6579/periodos"
OPEN_DATASUS_API_URL = (
    "https://apidadosabertos.saude.gov.br/vigilancia-e-meio-ambiente/"
    "sistema-de-informacao-sobre-mortalidade?limit=1&offset=0"
)
OPEN_DATASUS_2024_ZIP_URL = (
    "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/DO24OPEN_csv.zip"
)
DATASUS_HOST = "ftp.datasus.gov.br"
DATASUS_DIRECTORIES = {
    "final": "/dissemin/publicos/SIM/CID10/DORES",
    "preliminary": "/dissemin/publicos/SIM/PRELIM/DORES",
}
USER_AGENT = "enceladus-dependency-probe/1.0"


@dataclass(frozen=True)
class ProbeResult:
    name: str
    ok: bool
    elapsed_ms: int
    detail: str


class DownloadSampleComplete(Exception):
    pass


def timed_probe(name: str, probe: Callable[[], str]) -> ProbeResult:
    started = time.monotonic()
    try:
        detail = probe()
        ok = True
    except Exception as error:
        detail = f"{type(error).__name__}: {error}"
        ok = False
    return ProbeResult(name, ok, round((time.monotonic() - started) * 1000), detail)


def probe_dns(host: str) -> str:
    addresses = sorted({item[4][0] for item in socket.getaddrinfo(host, None)})
    return ", ".join(addresses)


def fetch_json(url: str, timeout: int, ssl_context: ssl.SSLContext) -> object:
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=timeout, context=ssl_context) as response:
        if response.status != 200:
            raise RuntimeError(f"HTTP {response.status}")
        body = response.read()
        if response.headers.get("Content-Encoding", "").lower() == "gzip":
            body = gzip.decompress(body)
        return json.loads(body)


def probe_sidra(timeout: int, ssl_context: ssl.SSLContext) -> str:
    payload = fetch_json(SIDRA_URL, timeout, ssl_context)
    if not isinstance(payload, list) or len(payload) < 2:
        raise ValueError("response has no population records")
    record = payload[1]
    if not {"D1C", "D1N", "V", "D3C"}.issubset(record):
        raise ValueError("response schema does not match table 6579")
    return f"HTTP 200; period={record['D3C']}; municipality={record['D1N']}; population={record['V']}"


def probe_ibge_periods(timeout: int, ssl_context: ssl.SSLContext) -> str:
    payload = fetch_json(IBGE_PERIODS_URL, timeout, ssl_context)
    if not isinstance(payload, list) or not payload:
        raise ValueError("response contains no table 6579 periods")
    periods = sorted(int(item["id"]) for item in payload)
    return f"HTTP 200; latest periods={periods[-5:]}"


def probe_open_datasus_api(timeout: int, ssl_context: ssl.SSLContext) -> str:
    payload = fetch_json(OPEN_DATASUS_API_URL, timeout, ssl_context)
    if not isinstance(payload, (dict, list)) or not payload:
        raise ValueError("API returned no SIM record")
    return "HTTP 200; returned a SIM record with limit=1"


def probe_open_datasus_zip(timeout: int, ssl_context: ssl.SSLContext) -> str:
    request = urllib.request.Request(
        OPEN_DATASUS_2024_ZIP_URL,
        headers={"Range": "bytes=0-65535", "User-Agent": USER_AGENT},
    )
    with urllib.request.urlopen(request, timeout=timeout, context=ssl_context) as response:
        sample = response.read(65_536)
        status = response.status
    if not sample.startswith(b"PK"):
        raise ValueError("response does not have a ZIP signature")
    return f"HTTP {status}; downloaded {len(sample)} byte sample from definitive 2024 ZIP"


def datasus_years(file_names: list[str]) -> list[int]:
    return sorted({
        int(match.group(1))
        for file_name in file_names
        if (match := re.fullmatch(r"DO[A-Z]{2}(\d{4})\.DBC", file_name, re.IGNORECASE))
    })


def latest_datasus_file(file_names: list[str], state: str = "DF") -> str:
    candidates = [
        (int(match.group(1)), file_name)
        for file_name in file_names
        if (match := re.fullmatch(
            rf"DO{re.escape(state)}(\d{{4}})\.DBC", file_name, re.IGNORECASE
        ))
    ]
    if not candidates:
        raise ValueError(f"directory contains no SIM-DO files for state {state}")
    return max(candidates)[1]


def list_datasus(directory: str, timeout: int) -> tuple[list[str], str]:
    with ftplib.FTP(DATASUS_HOST, timeout=timeout) as ftp:
        ftp.login()
        ftp.cwd(directory)
        file_names = ftp.nlst()
    years = datasus_years(file_names)
    if not years:
        raise ValueError("directory contains no SIM-DO DBC files")
    return file_names, f"listed {len(file_names)} files; published years={years[0]}–{years[-1]}"


def probe_datasus_download(file_name: str, directory: str, timeout: int) -> str:
    received = 0

    def receive(chunk: bytes) -> None:
        nonlocal received
        received += len(chunk)
        if received >= 65_536:
            raise DownloadSampleComplete

    with ftplib.FTP(DATASUS_HOST, timeout=timeout) as ftp:
        ftp.login()
        ftp.cwd(directory)
        try:
            ftp.retrbinary(f"RETR {file_name}", receive)
        except DownloadSampleComplete:
            pass
    if received == 0:
        raise ValueError("download returned no bytes")
    return f"downloaded {received} byte sample from {file_name} over passive FTP"


def probe_app_api(api_url: str, timeout: int, ssl_context: ssl.SSLContext) -> str:
    base_url = api_url.rstrip("/")
    health = fetch_json(f"{base_url}/health", timeout, ssl_context)
    years = fetch_json(f"{base_url}/config/anos", timeout, ssl_context)
    reports = fetch_json(f"{base_url}/relatorios/processados", timeout, ssl_context)
    if health != {"status": "healthy"}:
        raise ValueError(f"unexpected health response: {health!r}")
    if not isinstance(years, list) or not years:
        raise ValueError("API returned no available years")
    if not isinstance(reports, list):
        raise ValueError("processed reports response is not a list")
    return f"healthy; year cap={years[-1]}; processed reports={len(reports)}"


def probe_ses(region: str) -> str:
    try:
        import boto3
    except ImportError as error:
        raise RuntimeError("install the project dependencies to check SES") from error
    client = boto3.client("ses", region_name=region)
    enabled = client.get_account_sending_enabled()["Enabled"]
    quota = client.get_send_quota()
    return (
        f"sending_enabled={enabled}; sent_last_24h={quota['SentLast24Hours']}; "
        f"max_24h={quota['Max24HourSend']}"
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api-url", default=os.getenv("ENCELADUS_API_URL"))
    parser.add_argument("--timeout", type=int, default=20)
    parser.add_argument("--ca-bundle", help="PEM CA bundle for HTTPS verification")
    parser.add_argument(
        "--insecure",
        action="store_true",
        help="disable HTTPS certificate verification for connectivity diagnosis only",
    )
    parser.add_argument("--check-ses", action="store_true")
    parser.add_argument("--ses-region", default="eu-west-1")
    parser.add_argument("--json", action="store_true", dest="as_json")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.insecure:
        ssl_context = ssl._create_unverified_context()
    else:
        ssl_context = ssl.create_default_context(cafile=args.ca_bundle)
    results = [
        timed_probe("dns:apisidra.ibge.gov.br", lambda: probe_dns("apisidra.ibge.gov.br")),
        timed_probe(
            "sidra:population-table-6579",
            lambda: probe_sidra(args.timeout, ssl_context),
        ),
        timed_probe(
            "ibge:table-6579-periods",
            lambda: probe_ibge_periods(args.timeout, ssl_context),
        ),
        timed_probe("dns:ftp.datasus.gov.br", lambda: probe_dns(DATASUS_HOST)),
    ]

    final_files: list[str] = []
    for label, directory in DATASUS_DIRECTORIES.items():
        result_files: list[str] = []

        def directory_probe(directory: str = directory) -> str:
            nonlocal result_files
            result_files, detail = list_datasus(directory, args.timeout)
            return detail

        result = timed_probe(f"datasus:{label}-listing", directory_probe)
        results.append(result)
        if label == "final" and result.ok:
            final_files = result_files

    if final_files:
        results.append(timed_probe(
            "datasus:dbc-download",
            lambda: probe_datasus_download(
                latest_datasus_file(final_files),
                DATASUS_DIRECTORIES["final"],
                args.timeout,
            ),
        ))

    results.extend([
        timed_probe(
            "alternative:open-datasus-api",
            lambda: probe_open_datasus_api(args.timeout, ssl_context),
        ),
        timed_probe(
            "alternative:open-datasus-2024-zip",
            lambda: probe_open_datasus_zip(args.timeout, ssl_context),
        ),
    ])

    if args.api_url:
        results.append(timed_probe(
            "enceladus:api",
            lambda: probe_app_api(args.api_url, args.timeout, ssl_context),
        ))
    if args.check_ses:
        results.append(timed_probe("aws:ses", lambda: probe_ses(args.ses_region)))

    if args.as_json:
        print(json.dumps([asdict(result) for result in results], indent=2))
    else:
        if args.insecure:
            print("[WARN] HTTPS certificate verification is disabled (--insecure)")
        for result in results:
            marker = "PASS" if result.ok else "FAIL"
            print(f"[{marker}] {result.name} ({result.elapsed_ms} ms): {result.detail}")
        if not args.api_url:
            print("[SKIP] enceladus:api: pass --api-url or set ENCELADUS_API_URL")
        if not args.check_ses:
            print("[SKIP] aws:ses: pass --check-ses for a read-only account check")

    return 0 if all(result.ok for result in results) else 1


if __name__ == "__main__":
    sys.exit(main())
