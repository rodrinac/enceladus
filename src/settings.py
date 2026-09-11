import os
from dataclasses import dataclass
from pathlib import Path

SOURCE_ROOT = Path(__file__).resolve().parent
APP_HOME = Path("/var/lib/enceladus").resolve()


def _get_path(name: str, default: Path) -> Path:
    return Path(os.getenv(name, default)).expanduser().resolve()


def _get_origins() -> str | list[str]:
    value = os.getenv("CORS_ORIGINS", "*").strip()
    if value == "*":
        return value

    return [origin.strip() for origin in value.split(",") if origin.strip()]


@dataclass(frozen=True)
class Settings:
    app_home: Path
    config_path: Path
    cors_origins: str | list[str]
    data_dir: Path
    datasus_max_year_path: Path
    population_data_path: Path
    redis_db: int
    redis_host: str
    redis_password: str | None
    redis_port: int
    reports_dir: Path
    r_scripts_dir: Path
    ses_configuration_set: str | None
    ses_region: str
    ses_sender: str

    @classmethod
    def from_environment(cls) -> "Settings":
        app_home = _get_path("ENCELADUS_HOME", APP_HOME)

        return cls(
            app_home=app_home,
            config_path=SOURCE_ROOT / "config.yml",
            cors_origins=_get_origins(),
            data_dir=SOURCE_ROOT / "data",
            datasus_max_year_path=_get_path(
                "ENCELADUS_DATASUS_MAX_YEAR_PATH",
                app_home / "data" / "datasus-max-year.txt",
            ),
            population_data_path=_get_path(
                "ENCELADUS_POPULATION_DATA_PATH",
                app_home / "data" / "population.csv",
            ),
            redis_db=int(os.getenv("REDIS_DB", "0")),
            redis_host=os.getenv("REDIS_HOST", "localhost"),
            redis_password=os.getenv("REDIS_PASSWORD"),
            redis_port=int(os.getenv("REDIS_PORT", "6379")),
            reports_dir=app_home / "relatorios",
            r_scripts_dir=SOURCE_ROOT / "rscripts",
            ses_configuration_set=os.getenv("SES_CONFIGURATION_SET", "Default") or None,
            ses_region="eu-west-1",
            ses_sender=os.getenv(
                "SES_SENDER",
                "Enceladus Big Data <enceladus.bigdata@hotmail.com>",
            ),
        )


settings = Settings.from_environment()
