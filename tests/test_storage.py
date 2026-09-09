import storage
from settings import settings


def test_redis_client_uses_runtime_settings() -> None:
    connection_settings = storage.redis_client.connection_pool.connection_kwargs

    assert connection_settings["host"] == settings.redis_host
    assert connection_settings["port"] == settings.redis_port
    assert connection_settings["db"] == settings.redis_db
    assert connection_settings["password"] == settings.redis_password
