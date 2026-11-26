# OVH 邮件中继

`relay.py` 是可选的私有 Webhook → SMTP 中继，仅依赖 Python 标准库。邮件主题采用 `OVH · <正文首个非空行>`，正文提供 HTML 分组样式和完整的纯文本备选。中继不改变 OVH 控制台的通知触发条件，也不改变 Telegram 或其他 Webhook 的消息。

- 在 OVH 控制台同一私有网络中运行 `python -u /relay.py`，不要向公网发布 `8080`。
- 通过受保护的环境配置提供 `RELAY_TOKEN`、`SMTP_HOST`、`SMTP_PORT`（默认 `587`）、`SMTP_USERNAME`、`SMTP_PASSWORD`、`SMTP_FROM` 和 `SMTP_TO`（多收件人用逗号分隔）。SMTP 使用 STARTTLS；不要把真实值提交到仓库。
- 在「设置 → 通知通道」将 Webhook 指向 `http://<私有中继主机名>:8080/notify/<RELAY_TOKEN>`。请求正文中的 `text` 用于生成邮件；只有上游 SMTP 接受发信才返回成功。重新检测配置不会发送测试邮件，手动「发送测试通知」才会实际发信。

本地验证（SMTP 已模拟，不会发真实邮件）：

```sh
python3 -B -m unittest discover -s deploy/mail-relay -v
```
