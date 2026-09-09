import logging
from email.mime.application import MIMEApplication
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from functools import lru_cache

import boto3

from settings import settings

logger = logging.getLogger(__name__)


@lru_cache(maxsize=1)
def _ses_client():
    return boto3.client('ses', region_name=settings.ses_region)


def send_email(to: str, subject: str, file_name: str, attachment: bytes):
    SENDER = settings.ses_sender
    RECIPIENT = to
    SUBJECT = subject

    BODY_TEXT = (f'{subject}\r\n'
                 'This email was sent with Amazon SES using the '
                 'AWS SDK for Python (Boto).'
                 )

    # The HTML body of the email.
    BODY_HTML = f"""<html>
    <head></head>
    <body>
    <h1>{subject}</h1>
    <p>This email was sent with
        <a href='https://aws.amazon.com/ses/'>Amazon SES</a> using the
        <a href='https://aws.amazon.com/sdk-for-python/'>
        AWS SDK for Python (Boto)</a>.</p>
    </body>
    </html>
                """

    CHARSET = 'UTF-8'

    ATTACHMENT = file_name

    msg = MIMEMultipart('mixed')

    msg['Subject'] = SUBJECT
    msg['From'] = SENDER
    msg['To'] = RECIPIENT

    msg_body = MIMEMultipart('alternative')

    textpart = MIMEText(BODY_TEXT.encode(CHARSET), 'plain', CHARSET)
    htmlpart = MIMEText(BODY_HTML.encode(CHARSET), 'html', CHARSET)

    msg_body.attach(textpart)
    msg_body.attach(htmlpart)

    att = MIMEApplication(attachment)

    att.add_header('Content-Disposition', 'attachment', filename=ATTACHMENT)

    msg.attach(msg_body)

    msg.attach(att)

    request = {
        'Destinations': [RECIPIENT],
        'RawMessage': {'Data': msg.as_string()},
        'Source': SENDER,
    }
    if settings.ses_configuration_set:
        request['ConfigurationSetName'] = settings.ses_configuration_set

    logger.info('Enviando relatório por e-mail com destinatário para %s.', to)
    response = _ses_client().send_raw_email(**request)
    logger.info("E-mail enviado! ID da mensagem: %s", response['MessageId'])
