import importlib
import logging
from unittest.mock import Mock

import pytest


@pytest.mark.parametrize(
    ("module_name", "function_name", "states"),
    [
        ("densidade_municipal_por_periodo", "preparar_e_enviar_relatorio_async", "DF"),
        ("densidade_municipal_por_periodo_geral", "preparar_e_enviar_relatorio_async", ["DF"]),
        ("casos_mensais_por_municipio_por_estado", "preparar_e_enviar_diagrama_async", ["DF"]),
    ],
)
def test_failed_report_logs_error_without_sending_email(
    module_name, function_name, states, tmp_path, monkeypatch, caplog,
):
    module = importlib.import_module(f"relatorios.{module_name}")
    monkeypatch.setattr(module, "relatorios_folder", tmp_path)
    process = Mock(returncode=1, stdout="year_start must be a single whole number")
    monkeypatch.setattr(module, "run_rscript", Mock(return_value=process))
    send_email = Mock()
    save_timestamp = Mock()
    monkeypatch.setattr(module, "send_email", send_email)
    monkeypatch.setattr(module.storage, "salvar_data_processamento", save_timestamp)

    with caplog.at_level(logging.ERROR, logger=module.__name__):
        getattr(module, function_name)(states, "2024", "2024", "test@example.invalid", "request-123")

    assert "year_start must be a single whole number" in caplog.text
    assert "request-123" in caplog.text
    send_email.assert_not_called()
    save_timestamp.assert_not_called()
