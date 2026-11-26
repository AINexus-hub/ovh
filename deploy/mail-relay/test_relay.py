import json
import os
import threading
import unittest
from email import policy
from email.parser import BytesParser
from http.client import HTTPConnection
from unittest.mock import patch

import relay


RESTOCK = """🎉 VPS补货通知

套餐: vps-2027-model2-ca
子公司: US / US 区(api.us.ovhcloud.com)
时间: 2026-11-26 18:02:14

1. EU-WEST-RBX (eu-west-rbx)
   状态: 现货
2. US-EAST-VA (us-east-va)
   状态: 无货 | 预计交付: 3天

💡 快去抢购吧！"""


class MailFormattingTests(unittest.TestCase):
    def build(self, content):
        message = relay.build_message(content, 'sender@example.test', ['recipient@example.test'])
        return BytesParser(policy=policy.default).parsebytes(message.as_bytes())

    def test_subject_and_alternative_bodies(self):
        message = self.build(RESTOCK)
        self.assertEqual(message['Subject'], 'OVH · 🎉 VPS补货通知')
        self.assertEqual(message.get_content_type(), 'multipart/alternative')
        self.assertEqual(message.get_body(('plain',)).get_content(), RESTOCK + '\n')
        html = message.get_body(('html',)).get_content()
        self.assertIn('🎉 VPS补货通知</h1>', html)
        self.assertEqual(html.count('<table '), 3)
        for text in ('vps-2027-model2-ca', 'US / US 区(api.us.ovhcloud.com)', '2026-11-26 18:02:14',
                     '1. EU-WEST-RBX (eu-west-rbx)', '2. US-EAST-VA (us-east-va)', '现货', '预计交付: 3天'):
            self.assertIn(text, html)

    def test_title_uses_first_nonblank_line_and_stays_single_line(self):
        message = self.build('\r\n  📦  下架通知\r\nBcc: other@example.test')
        self.assertEqual(message['Subject'], 'OVH · 📦 下架通知')
        self.assertNotIn('Bcc', message)
        self.assertEqual(self.build('标题' * 90)['Subject'], 'OVH · ' + ('标题' * 60)[:119] + '…')
        self.assertEqual(self.build('单行通知')['Subject'], 'OVH · 单行通知')

    def test_html_escapes_title_fields_and_unstructured_lines(self):
        message = self.build('<img src=x>\n\n套餐: <script>x</script> & a\n普通文字 <b>x</b>')
        html = message.get_body(('html',)).get_content()
        self.assertIn('&lt;img src=x&gt;', html)
        self.assertIn('&lt;script&gt;x&lt;/script&gt; &amp; a', html)
        self.assertIn('普通文字 &lt;b&gt;x&lt;/b&gt;', html)
        for tag in ('<img', '<script', '<b>'):
            self.assertNotIn(tag, html)

    def test_https_order_link_remains_clickable(self):
        message = self.build('🎉 下单成功\n\n订单链接: https://example.test/?a=1&b=2\n其他: javascript:alert(1)')
        html = message.get_body(('html',)).get_content()
        self.assertIn('href="https://example.test/?a=1&amp;b=2"', html)
        self.assertNotIn('href="javascript:', html)


class RelayTests(unittest.TestCase):
    def test_authenticated_delivery_with_mock_smtp(self):
        env = {
            'RELAY_TOKEN': 'test-token', 'SMTP_FROM': 'sender@example.test',
            'SMTP_TO': 'recipient@example.test, second@example.test', 'SMTP_HOST': 'smtp.example.test',
            'SMTP_PORT': '587', 'SMTP_USERNAME': 'test-user', 'SMTP_PASSWORD': 'test-password',
        }
        with patch.dict(os.environ, env), patch.object(relay.smtplib, 'SMTP') as smtp_factory:
            smtp = smtp_factory.return_value.__enter__.return_value
            smtp.send_message.return_value = {}
            with relay.RelayServer(('127.0.0.1', 0), relay.Relay) as server:
                worker = threading.Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01})
                worker.start()
                self.addCleanup(worker.join)
                try:
                    conn = HTTPConnection(*server.server_address, timeout=2)
                    self.addCleanup(conn.close)
                    conn.request('POST', '/notify/wrong', body=json.dumps({'text': RESTOCK}))
                    response = conn.getresponse()
                    self.assertEqual(response.status, 404)
                    response.read()
                    smtp_factory.assert_not_called()

                    conn.request('POST', '/notify/test-token', body=json.dumps({'text': RESTOCK}))
                    response = conn.getresponse()
                    self.assertEqual(response.status, 200)
                    self.assertEqual(json.load(response), {'code': 0})
                finally:
                    server.shutdown()
            smtp.starttls.assert_called_once()
            smtp.login.assert_called_once_with('test-user', 'test-password')
            smtp.send_message.assert_called_once()
            message, = smtp.send_message.call_args.args
            self.assertEqual(message['Subject'], 'OVH · 🎉 VPS补货通知')
            self.assertEqual(message.get_content_type(), 'multipart/alternative')
            self.assertEqual(smtp.send_message.call_args.kwargs, {
                'from_addr': 'sender@example.test', 'to_addrs': ['recipient@example.test', 'second@example.test'],
            })


if __name__ == '__main__':
    unittest.main()
