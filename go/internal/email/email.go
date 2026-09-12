// Package email mirrors src/send_email.py: sends report PDFs through Amazon SES.
package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"

	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type Sender interface {
	Send(to, subject, fileName string, attachment []byte) error
}

// SES implements Sender on top of the AWS SES SendRawEmail API, producing a
// mixed-multipart(alternative(text,html),pdf) message equivalent to the
// Python MIMEMultipart stack.
type SES struct {
	Client           *ses.Client
	ConfigurationSet string
	SenderAddress    string
}

func (s *SES) Send(to, subject, fileName string, attachment []byte) error {
	if s.SenderAddress == "" {
		return fmt.Errorf("SES sender address is not configured")
	}

	bodyText := subject + "\r\n" +
		"This email was sent with Amazon SES using the " +
		"AWS SDK for Python (Boto)."
	bodyHTML := fmt.Sprintf(`<html>
    <head></head>
    <body>
    <h1>%s</h1>
    <p>This email was sent with
        <a href='https://aws.amazon.com/ses/'>Amazon SES</a> using the
        <a href='https://aws.amazon.com/sdk-for-python/'>
        AWS SDK for Python (Boto)</a>.</p>
    </body>
    </html>`, subject)

	var buffer bytes.Buffer
	root := multipart.NewWriter(&buffer)

	rootBoundary := root.Boundary()
	if _, err := fmt.Fprintf(&buffer, "MIME-Version: 1.0\r\n"+
		"From: %s\r\nTo: %s\r\nSubject: %s\r\n"+
		"Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n",
		s.SenderAddress, to, subject, rootBoundary); err != nil {
		return err
	}

	alternativeSection, err := root.CreatePart(map[string][]string{
		"Content-Type": {"multipart/alternative"},
	})
	if err != nil {
		return err
	}
	alternative := multipart.NewWriter(alternativeSection)
	if _, err := fmt.Fprintf(alternativeSection,
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n",
		alternative.Boundary()); err != nil {
		return err
	}

	writeSection := func(w *multipart.Writer, contentType string, payload []byte) error {
		part, err := w.CreatePart(map[string][]string{
			"Content-Type":              {contentType + "; charset=UTF-8"},
			"Content-Transfer-Encoding": {"base64"},
			"MIME-Version":              {"1.0"},
		})
		if err != nil {
			return err
		}
		_, err = part.Write([]byte(base64.StdEncoding.EncodeToString(payload)))
		return err
	}

	if err := writeSection(alternative, "text/plain", []byte(bodyText)); err != nil {
		return err
	}
	if err := writeSection(alternative, "text/html", []byte(bodyHTML)); err != nil {
		return err
	}
	if err := alternative.Close(); err != nil {
		return err
	}

	pdfPart, err := root.CreatePart(map[string][]string{
		"Content-Type":              {"application/octet-stream"},
		"Content-Transfer-Encoding": {"base64"},
		"MIME-Version":              {"1.0"},
		"Content-Disposition":       {"attachment; filename=\"" + fileName + "\""},
	})
	if err != nil {
		return err
	}
	if _, err := pdfPart.Write([]byte(base64.StdEncoding.EncodeToString(attachment))); err != nil {
		return err
	}
	if err := root.Close(); err != nil {
		return err
	}

	input := &ses.SendRawEmailInput{
		Source:       &s.SenderAddress,
		Destinations: []string{to},
		RawMessage:   &types.RawMessage{Data: buffer.Bytes()},
	}
	if s.ConfigurationSet != "" {
		input.ConfigurationSetName = &s.ConfigurationSet
	}

	result, err := s.Client.SendRawEmail(context.Background(), input)
	if err != nil {
		return err
	}
	if result.MessageId == nil {
		return fmt.Errorf("SES returned no message id")
	}
	return nil
}
