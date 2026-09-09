import logging
from subprocess import PIPE, STDOUT, Popen

import storage
from send_email import send_email
from settings import SOURCE_ROOT, settings

logger = logging.getLogger(__name__)

relatorios_folder = settings.reports_dir / "queimaduras" / "casos-mensais-por-municipio-por-estado"

id_relatorio = 'CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO'

relatorios_folder.mkdir(parents=True, exist_ok=True)


def _formatar_intervalo(ano_inicio: int, ano_fim: int):
    if ano_inicio == ano_fim:
        return f'em {ano_fim}'

    return f'entre {ano_inicio} e {ano_fim}'


def ler_relatorio(relatorio: str):
    with (relatorios_folder / relatorio).open('rb') as f:
        return f.read()

def preparar_e_enviar_diagrama_async(estados: str, ano_inicio: str, ano_fim: str, email: str, id_requisicao: str):

    logger.info('Obtendo registros de queimaduras para %s %s.',
                estados, _formatar_intervalo(ano_inicio, ano_fim))

    file_path = relatorios_folder / f'{"-".join(estados)}.{ano_inicio}.{ano_fim}.pdf'
    working_path = relatorios_folder / id_requisicao

    working_path.mkdir(parents=True, exist_ok=True)

    if not file_path.exists():
        p = Popen(['Rscript', settings.r_scripts_dir / 'casos_mensais_por_municipio_por_estado.R', ','.join(estados),
                   ano_inicio, ano_fim, file_path, working_path], stdout=PIPE, stdin=PIPE,
                   stderr=STDOUT, cwd=SOURCE_ROOT)

        streamdata = p.communicate()[0]

        logger.debug('Retorno da execução: %s', streamdata.decode())
        logger.info('Executou comando R com status %s.', p.returncode)

        if (p.returncode != 0):
            return

        storage.salvar_data_processamento(id_relatorio, file_path.name)

    logger.info('Preparando para envio do diagrama de %s em %s com destinatário a %s.', estados, [
                ano_inicio, ano_fim], email)

    with file_path.open('rb') as f:
        send_email(
            email, f'Diagrama de distribuição do local de falecimento para {", ".join(estados)}, {_formatar_intervalo(ano_inicio, ano_fim)}', 'relatorio.pdf', f.read())
