import asyncio
import logging.config
import uuid
from functools import partial

from quart import Quart, Response, jsonify, request
from quart_cors import cors

import job_status
from default_config import defaultConfig
from relatorios import (
    casos_mensais_por_municipio_por_estado,
    densidade_municipal_por_periodo,
    densidade_municipal_por_periodo_geral,
    relatorios,
)
from settings import settings

app = Quart(__name__)

app = cors(app, allow_origin=settings.cors_origins)

@app.route('/')
def root():
    return jsonify(service='enceladus-api', status='healthy')


@app.route('/health')
def health():
    return jsonify(status='healthy')


@app.route('/config/anos')
def anos_disponiveis():
    return jsonify(defaultConfig.anos_disponiveis())

@app.route('/config/estados')
def estados_disponiveis():
    return jsonify(defaultConfig.estados_disponiveis())

@app.route('/config/codigoscid10')
def codigos_cid10():
    return jsonify(defaultConfig.codigos_cid10())

@app.route('/config/relatorios')
def relatorios_disponiveis():
    return jsonify(defaultConfig.relatorios())


# Relatórios

@app.route('/relatorios/processados')
def get_relatorios_processados():
    return jsonify(relatorios.listar_relatorios_processados())


def _nome_relatorio(report_id: str) -> str:
    return next(report["nome"] for report in defaultConfig.relatorios() if report["id"] == report_id)


def _processar_relatorio(id_requisicao: str, worker, *args) -> None:
    job_status.mark_running(id_requisicao)
    try:
        success = worker(*args, id_requisicao)
    except Exception:
        logging.getLogger(__name__).exception("Falha ao processar requisição %s.", id_requisicao)
        job_status.mark_failed(id_requisicao)
        return

    if success:
        job_status.mark_succeeded(id_requisicao)
    else:
        job_status.mark_failed(id_requisicao)

## Densidade municipal por período geral


@app.route('/relatorios/queimaduras/densidade-municipal-por-periodo-geral/<path:path>')
async def get_relatorio_geral(path):
    arquivo = densidade_municipal_por_periodo_geral.ler_relatorio(path)

    response = Response(arquivo)
    response.headers.set('Content-Disposition',
                         'attachment', filename=path)
    response.headers.set('Content-Type', 'application/pdf')

    return response


@app.route('/relatorios/queimaduras/densidade-municipal-por-periodo-geral', methods=['POST'])
async def post_relatorio_queimaduras_geral():

    estados_param = request.args.getlist('estado')
    data_inicio_param = request.args.get('data_inicio')
    data_fim_param = request.args.get('data_fim')
    email_param = request.args.get('email')

    id_req = str(uuid.uuid1())
    job_status.register(id_req, _nome_relatorio('DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL'), estados_param,
                        data_inicio_param, data_fim_param)

    asyncio.get_event_loop().run_in_executor(
        None,
        partial(_processar_relatorio, id_req, densidade_municipal_por_periodo_geral.preparar_e_enviar_relatorio_async,
                estados_param, data_inicio_param, data_fim_param, email_param),
    )

    return dict(destino=email_param, id_requisicao=id_req), 202


## Densidade municipal por período

@app.route('/relatorios/queimaduras/densidade-municipal-por-periodo/<path:path>')
async def get_relatorio(path):
    arquivo = densidade_municipal_por_periodo.ler_relatorio(path)

    response = Response(arquivo)
    response.headers.set('Content-Disposition',
                         'attachment', filename=path)
    response.headers.set('Content-Type', 'application/pdf')

    return response


@app.route('/relatorios/queimaduras/densidade-municipal-por-periodo', methods=['POST'])
async def post_relatorio_queimaduras():

    estados_param = request.args.get('estado')
    data_inicio_param = request.args.get('data_inicio')
    data_fim_param = request.args.get('data_fim')
    email_param = request.args.get('email')

    id_req = str(uuid.uuid1())
    job_status.register(id_req, _nome_relatorio('DENSIDADE_MUNICIPAL_POR_PERIODO'), [estados_param],
                        data_inicio_param, data_fim_param)

    asyncio.get_event_loop().run_in_executor(
        None,
        partial(_processar_relatorio, id_req, densidade_municipal_por_periodo.preparar_e_enviar_relatorio_async,
                estados_param, data_inicio_param, data_fim_param, email_param),
    )

    return dict(destino=email_param, id_requisicao=id_req), 202


## Casos Mensais por Município por Estado

@app.route('/relatorios/queimaduras/casos-mensais-por-municipio-por-estado/<path:path>')
async def get_relatorio_2(path):
    arquivo = casos_mensais_por_municipio_por_estado.ler_relatorio(path)

    response = Response(arquivo)
    response.headers.set('Content-Disposition',
                         'attachment', filename=path)
    response.headers.set('Content-Type', 'application/pdf')

    return response


@app.route('/relatorios/queimaduras/casos-mensais-por-municipio-por-estado', methods=['POST'])
async def post_relatorio_queimaduras_2():

    estados_param = request.args.getlist('estado')
    ano_inicio_param = request.args.get('ano_inicio')
    ano_fim_param = request.args.get('ano_fim')
    email_param = request.args.get('email')

    id_req = str(uuid.uuid1())
    job_status.register(id_req, _nome_relatorio('CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO'), estados_param,
                        ano_inicio_param, ano_fim_param)

    asyncio.get_event_loop().run_in_executor(
        None,
        partial(_processar_relatorio, id_req, casos_mensais_por_municipio_por_estado.preparar_e_enviar_diagrama_async,
                estados_param, ano_inicio_param, ano_fim_param, email_param),
    )

    return dict(destino=email_param, id_requisicao=id_req), 202

logging_config = {
    'version': 1,
    'disable_existing_loggers': False,
    'formatters': {
        'standard': {
            'format': '%(asctime)s [%(levelname)s] %(name)s: %(message)s'
        },
    },
    'handlers': {
        'console_handler': {
            'class': 'logging.StreamHandler',
            'level': 'INFO',
            'formatter': 'standard',
        },
    },
    'loggers': {
        '': {
            'handlers': ['console_handler'],
            'level': 'DEBUG',
            'propagate': False
        }
    }
}

logging.config.dictConfig(logging_config)
