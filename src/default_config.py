from datetime import date
from pathlib import Path

import yaml

from settings import settings


class DefaultConfig:

    def __init__(self, config_path: Path | None = None):
        self.__config_path = config_path or settings.config_path
        self.__config = self.__read_yaml()

    def anos_disponiveis(self) -> list[int]:
        if "anoInicio" in self.__config:
            return list(range(self.__config["anoInicio"], date.today().year + 1))
        return self.__config.get("anos", [])

    def estados_disponiveis(self) -> list:
        return self.__config.get("estados")

    def codigos_cid10(self) -> dict:
        return self.__config.get("codigosCid10")

    def relatorios(self) -> list:
        return self.__config.get("relatorios")

    def __read_yaml(self):
        with self.__config_path.open(encoding="utf-8") as stream:
            return yaml.safe_load(stream) or {}


defaultConfig = DefaultConfig()
