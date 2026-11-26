"""Private Webhook -> STARTTLS SMTP relay for OVH Console notifications."""

import json
import os
import re
import secrets
import smtplib
import ssl
from email.message import EmailMessage
from html import escape
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit


FIELD = re.compile(r"^([\w /()（）.-]{1,24})(?:：\s*|:\s+)(.+)$")
ITEM = re.compile(r"^\d+\.\s+\S")


def render_value(value):
    text = escape(value)
    if value.startswith('https://') and not any(c.isspace() or ord(c) < 32 for c in value):
        try:
            url = urlsplit(value)
            if url.hostname and url.username is None:
                return f'<a href="{text}" style="color:#1d4ed8;text-decoration:underline;">{text}</a>'
        except ValueError:
            pass
    return text


def render_details(lines):
    parts = []
    rows = []

    def flush_rows():
        if rows:
            parts.append('<table role="presentation" width="100%" cellspacing="0" cellpadding="0">'
                         + "".join(rows) + '</table>')
            rows.clear()

    for line in lines:
        field = FIELD.fullmatch(line)
        if field:
            name, value = field.groups()
            css = 'padding:5px 0;vertical-align:top;'
            rows.append(f'<tr><td style="{css}width:6em;color:#475569;">{escape(name)}</td>'
                        f'<td style="{css}color:#0f172a;overflow-wrap:anywhere;">{render_value(value)}</td></tr>')
        else:
            flush_rows()
            parts.append(f'<div style="padding:3px 0;overflow-wrap:anywhere;">{escape(line)}</div>')
    flush_rows()
    return "".join(parts)


def render_html(title, lines):
    blocks = []
    block = []
    for line in [*lines, ""]:
        line = line.strip()
        if block and (not line or ITEM.match(line)):
            blocks.append(block)
            block = []
        if line:
            block.append(line)

    sections = []
    for block in blocks:
        heading = ""
        if ITEM.match(block[0]):
            heading = f'<div style="font-weight:bold;margin-bottom:4px;">{escape(block[0])}</div>'
            block = block[1:]
        sections.append('<div style="margin:12px 0 0;padding:12px 14px;'
                        'background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;">'
                        + heading + render_details(block) + '</div>')

    return ('<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width, initial-scale=1"></head>'
            '<body style="margin:0;background:#f1f5f9;">'
            '<div style="max-width:600px;margin:0 auto;padding:16px;">'
            '<div style="padding:18px;background:#ffffff;border:1px solid #e2e8f0;'
            'border-radius:12px;font:15px/1.55 system-ui,sans-serif;color:#0f172a;">'
            '<div style="font-size:12px;font-weight:bold;color:#475569;">OVH</div>'
            f'<h1 style="font-size:21px;line-height:1.35;margin:5px 0 14px;">{escape(title)}</h1>'
            + "".join(sections) + '</div></div></body></html>')


def build_message(content, sender, recipients):
    lines = content.strip().splitlines()
    title = lines[0].strip() if lines else "通知"
    # Keep mail headers to a single, concise line, independent of the body format.
    subject_title = " ".join(title.split())
    if len(subject_title) > 120:
        subject_title = subject_title[:119].rstrip() + "…"
    message = EmailMessage()
    message['From'] = sender
    message['To'] = ', '.join(recipients)
    message['Subject'] = 'OVH · ' + subject_title
    message.set_content(content)
    message.add_alternative(render_html(title, lines[1:]), subtype='html')
    return message


def deliver(content):
    recipients = [x.strip() for x in os.environ['SMTP_TO'].split(',') if x.strip()]
    sender = os.environ['SMTP_FROM']
    message = build_message(content, sender, recipients)
    with smtplib.SMTP(os.environ['SMTP_HOST'], int(os.environ.get('SMTP_PORT', '587')), timeout=7) as smtp:
        smtp.starttls(context=ssl.create_default_context())
        smtp.login(os.environ['SMTP_USERNAME'], os.environ['SMTP_PASSWORD'])
        refused = smtp.send_message(message, from_addr=sender, to_addrs=recipients)
    if refused:
        raise RuntimeError('SMTP refused one or more recipients')


class Relay(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        if not secrets.compare_digest(self.path, '/notify/' + os.environ['RELAY_TOKEN']):
            self.reply(404, {'code': 404})
            return
        try:
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 < size <= 16384:
                raise ValueError()
            content = json.loads(self.rfile.read(size))['text']
            if not isinstance(content, str) or not content.strip():
                raise ValueError()
        except (ValueError, KeyError, TypeError):
            self.reply(400, {'code': 400})
            return
        try:
            deliver(content)
        except Exception as e:
            print('SMTP delivery failed:', type(e).__name__, flush=True)
            self.reply(502, {'code': 502})
            return
        print('SMTP accepted notification', flush=True)
        self.reply(200, {'code': 0})


class RelayServer(HTTPServer):
    def get_request(self):
        conn, address = super().get_request()
        conn.settimeout(8)
        return conn, address


if __name__ == '__main__':
    RelayServer(('0.0.0.0', 8080), Relay).serve_forever()
