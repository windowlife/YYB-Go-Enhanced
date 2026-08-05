# 配置说明

## keepalive_interval

账号检查周期，默认 `30m`。

设置为 `0` 可以关闭后台保活。

## keepalive_ahead

Token 提前续期时间，默认 `45m`。

## ql_url

青龙 OpenAPI 地址。

青龙已将 5700 端口映射到 HAOS 主机时，可以填写：

```text
http://homeassistant.local:5700
