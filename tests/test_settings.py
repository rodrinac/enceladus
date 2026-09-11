from pathlib import Path

from settings import APP_HOME, Settings


def test_settings_are_loaded_from_environment(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setenv("ENCELADUS_POPULATION_DATA_PATH", str(tmp_path / "population.csv"))
    monkeypatch.setenv("ENCELADUS_HOME", str(tmp_path))
    monkeypatch.setenv("REDIS_HOST", "redis")
    monkeypatch.setenv("REDIS_PORT", "6380")
    monkeypatch.setenv("REDIS_DB", "3")
    monkeypatch.setenv("REDIS_PASSWORD", "test-password")
    monkeypatch.setenv("CORS_ORIGINS", "https://one.example, https://two.example")
    monkeypatch.setenv("SES_CONFIGURATION_SET", "")

    loaded_settings = Settings.from_environment()

    assert loaded_settings.app_home == tmp_path
    assert loaded_settings.population_data_path == tmp_path / "population.csv"
    assert loaded_settings.redis_host == "redis"
    assert loaded_settings.redis_port == 6380
    assert loaded_settings.redis_db == 3
    assert loaded_settings.redis_password == "test-password"
    assert loaded_settings.cors_origins == ["https://one.example", "https://two.example"]
    assert loaded_settings.ses_configuration_set is None


def test_runtime_paths_and_region_are_container_defaults() -> None:
    loaded_settings = Settings.from_environment()

    assert loaded_settings.reports_dir == APP_HOME / "relatorios"
    assert loaded_settings.ses_region == "eu-west-1"


def test_redis_defaults_target_a_local_runtime() -> None:
    loaded_settings = Settings.from_environment()

    assert loaded_settings.redis_host == "localhost"
    assert loaded_settings.redis_port == 6379
    assert loaded_settings.redis_db == 0
