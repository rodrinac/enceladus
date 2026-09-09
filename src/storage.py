from datetime import datetime

from redis import StrictRedis

from settings import settings

redis_client = StrictRedis(
    host=settings.redis_host,
    port=settings.redis_port,
    db=settings.redis_db,
    password=settings.redis_password,
    encoding="utf-8",
    decode_responses=True,
)
date_format = '%d/%m/%Y %H:%M:%S'

def salvar_data_processamento(id_relatorio, nome_relatorio: str):
  now = datetime.now()

  redis_client.set(f'dataProcessamento.{id_relatorio}.{nome_relatorio}', now.strftime(date_format))


def datas_processamento() -> dict:
  
  keys = redis_client.keys('dataProcessamento.*')

  return {key: redis_client.get(key) for key in keys}
