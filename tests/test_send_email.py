from unittest.mock import Mock, patch

from send_email import send_email


def test_send_email_uses_configured_ses_client() -> None:
    ses_client = Mock()
    ses_client.send_raw_email.return_value = {"MessageId": "message-id"}

    with patch("send_email._ses_client", return_value=ses_client):
        send_email(
            "recipient@example.com",
            "Report ready",
            "report.pdf",
            b"pdf-content",
        )

    request = ses_client.send_raw_email.call_args.kwargs
    assert request["Destinations"] == ["recipient@example.com"]
    assert request["RawMessage"]["Data"]
    assert request["Source"]
