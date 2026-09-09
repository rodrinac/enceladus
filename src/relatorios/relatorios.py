import glob
import logging
from datetime import datetime
from pathlib import Path

import storage
from default_config import defaultConfig
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
                data_processamento=datas_processamento.get(chave_redis)
            ))

    relatorios.sort(key=lambda relatorio: datetime.strptime(relatorio.get('data_processamento'), "%d/%m/%Y %H:%M:%S"), reverse=True)

    return relatorios
