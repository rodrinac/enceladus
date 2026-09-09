from pathlib import Path

import pytest
import yaml

from default_config import DefaultConfig


def test_loads_default_config_independently_of_working_directory(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    monkeypatch.chdir(tmp_path)

    config = DefaultConfig()

    assert 2019 in config.anos_disponiveis()
    assert config.relatorios()


def test_loads_an_explicit_config_file(tmp_path: Path) -> None:
    config_path = tmp_path / "config.yml"
    config_path.write_text(
        "anos: [2025]\nestados: []\ncodigosCid10: {}\nrelatorios: []\n",
        encoding="utf-8",
    )

    config = DefaultConfig(config_path)

    assert config.anos_disponiveis() == [2025]
    assert config.estados_disponiveis() == []
    assert config.codigos_cid10() == {}
    assert config.relatorios() == []


def test_invalid_yaml_is_not_silently_ignored(tmp_path: Path) -> None:
    config_path = tmp_path / "config.yml"
    config_path.write_text("anos: [", encoding="utf-8")

    with pytest.raises(yaml.YAMLError):
        DefaultConfig(config_path)
