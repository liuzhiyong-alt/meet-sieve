-- Codex 仅支持通过本机 HTTP(S) 代理访问外部服务；NULL 表示直连。
ALTER TABLE settings ADD COLUMN codex_proxy_port INTEGER NULL
    CHECK(codex_proxy_port IS NULL OR (codex_proxy_port >= 1 AND codex_proxy_port <= 65535));
