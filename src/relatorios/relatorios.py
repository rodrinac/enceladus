import glob
import logging
from datetime import datetime
from pathlib import Path

import storage
from default_config import defaultConfig
from job_status import list_jobs
from settings import settings

logger = logging.getLogger(__name__)

def listar_relatorios_processados():

    relatorios = []
    datas_processamento = storage.datas_processamento()

    for diretorio in defaultConfig.relatorios():
        report_subdir = Path(diretorio.get('path').lstrip('/')).relative_to('relatorios')
        path = str(settings.reports_dir / report_subdir / '*.pdf')
        
        for file in glob.glob(path):
            nome_base = Path(file).name
            partes_nome = nome_base.split('.')
            chave_redis = f"dataProcessamento.{diretorio.get('id')}.{nome_base}"

            relatorios.append(dict(
                tipo=diretorio.get('nome'),
                estado=partes_nome[0],
                data_inicio=partes_nome[1],
                data_fim=partes_nome[2],
                uri=f"{diretorio.get('path')}/{nome_base}",
                data_processamento=datas_processamento.get(chave_redis),
                id_requisicao=None,
                mensagem=None,
                status="succeeded",
                criado_em=datas_processamento.get(chave_redis),
            ))

    relatorios.extend(list_jobs())
    relatorios.sort(key=_ordenar_por_data, reverse=True)

    return relatorios


def _ordenar_por_data(relatorio):
    value = relatorio.get('criado_em') or relatorio.get('data_processamento')
    if not value:
        return datetime.min
    try:
        return datetime.fromisoformat(value)
    except ValueError:
        return datetime.strptime(value, "%d/%m/%Y %H:%M:%S")
