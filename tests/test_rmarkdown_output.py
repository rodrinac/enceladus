from pathlib import Path

R_SCRIPTS = Path(__file__).parents[1] / "src" / "rscripts"
REPORT_SCRIPTS = (
    "densidade_municipal_por_periodo.R",
    "densidade_municipal_por_periodo_geral.R",
    "casos_mensais_por_municipio_por_estado.R",
)


def test_rmarkdown_intermediates_use_writable_request_directory() -> None:
    for script_name in REPORT_SCRIPTS:
        script = (R_SCRIPTS / script_name).read_text(encoding="utf-8")

        assert "intermediates_dir = diretorio_de_trabalho" in script
