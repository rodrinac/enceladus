import logging
import shutil
from subprocess import PIPE, STDOUT, Popen

import storage
from default_config import defaultConfig
from send_email import send_email
from settings import SOURCE_ROOT, settings

logger = logging.getLogger(__name__)

relatorios_folder = settings.reports_dir / "queimaduras" / "densidade-municipal-por-periodo-geral"

id_relatorio = 'DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL'

relatorios_folder.mkdir(parents=True, exist_ok=True)


def _formatar_intervalo(data_inicio: str, data_fim: str):
    if data_inicio == data_fim:
        return f'em {data_inicio}'

    return f'entre {data_inicio} e {data_fim}'

def ler_relatorio(relatorio: str):
    with (relatorios_folder / relatorio).open('rb') as f:
        return f.read()


def preparar_e_enviar_relatorio_async(estados: list, data_inicio: str, data_fim: str, email: str, id_requisicao: str):

    if 'TODOS' in estados:
        estados = [k for e in defaultConfig.estados_disponiveis()
                   for k in e.keys()]

    logger.info('Obtendo registros de queimaduras para %s %s.',
                estados, _formatar_intervalo(data_inicio, data_fim))

    file_path = relatorios_folder / f"{'-'.join(estados)}.{data_inicio}.{data_fim}.pdf"
    working_path = relatorios_folder / id_requisicao

    working_path.mkdir(parents=True, exist_ok=True)

    if not file_path.exists():
        p = Popen(['Rscript', settings.r_scripts_dir / 'densidade_municipal_por_periodo_geral.R', ','.join(estados),
                  data_inicio, data_fim, file_path, working_path], stdout=PIPE, stdin=PIPE,
                  stderr=STDOUT, cwd=SOURCE_ROOT)

        streamdata = p.communicate()[0]

        logger.debug('Retorno da execução: %s', streamdata.decode())
        logger.info('Executou comando R com status %s.', p.returncode)

        if (p.returncode != 0):
            logger.error(
                'Falha ao gerar relatório %s (requisição %s): %s',
                id_relatorio, id_requisicao, streamdata.decode(errors='replace'),
            )
            return
        
        storage.salvar_data_processamento(id_relatorio, file_path.name)

    
    shutil.rmtree(working_path, ignore_errors=True)

    logger.info('Preparando para envio do diagrama de %s em %s com destinatário a %s.', estados, [
                data_inicio, data_fim], email)

    with file_path.open('rb') as f:
        send_email(
            email, f'Relatório de densidade municipal por período - {", ".join(estados)}, {_formatar_intervalo(data_inicio, data_fim)}', 'relatorio_densidade_municipal.pdf', f.read())
