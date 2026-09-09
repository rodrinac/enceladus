import asyncio
import importlib
from pathlib import Path
from unittest.mock import patch


def test_static_and_config_routes(tmp_path: Path) -> None:
    with patch.dict(
        "os.environ",
            {
                "ENCELADUS_HOME": str(tmp_path),
            },
    ):
        settings_module = importlib.import_module("settings")
        importlib.reload(settings_module)
        app = importlib.import_module("main").app

    async def request_routes() -> None:
        client = app.test_client()
        expected_mimetypes = {
            "/": "application/json",
            "/health": "application/json",
            "/config/anos": "application/json",
            "/config/estados": "application/json",
            "/config/codigoscid10": "application/json",
            "/config/relatorios": "application/json",
        }

        for path, expected_mimetype in expected_mimetypes.items():
            response = await client.get(path)

            assert response.status_code == 200
            assert response.mimetype == expected_mimetype

    asyncio.run(request_routes())
