import ipaddress

from fastapi import Request

from src.core.limiter import _client_ip


def _request(peer: str | None, headers: dict[str, str] | None = None) -> Request:
    scope = {
        "type": "http",
        "method": "POST",
        "path": "/api/v1/auth/login",
        "headers": [(k.lower().encode(), v.encode()) for k, v in (headers or {}).items()],
        "client": (peer, 12345) if peer else None,
        "query_string": b"",
    }
    return Request(scope)


def test_client_ip_trusted_peer_uses_real_ip(monkeypatch):
    monkeypatch.setattr("src.core.limiter._trusted_nets", [ipaddress.ip_network("10.0.0.0/8")])
    req = _request("10.0.0.5", {"X-Real-IP": "203.0.113.9"})
    assert _client_ip(req) == "203.0.113.9"


def test_client_ip_untrusted_peer_cannot_spoof(monkeypatch):
    # ревью фазы 17 (H1): прямой доступ к auth — подделанный X-Real-IP игнорируем
    monkeypatch.setattr("src.core.limiter._trusted_nets", [ipaddress.ip_network("10.0.0.0/8")])
    req = _request("192.0.2.7", {"X-Real-IP": "203.0.113.9"})
    assert _client_ip(req) == "192.0.2.7"


def test_client_ip_invalid_real_ip_falls_back_to_peer(monkeypatch):
    monkeypatch.setattr("src.core.limiter._trusted_nets", [ipaddress.ip_network("10.0.0.0/8")])
    req = _request("10.0.0.5", {"X-Real-IP": "not-an-ip"})
    assert _client_ip(req) == "10.0.0.5"


def test_client_ip_no_trusted_nets_keys_by_peer(monkeypatch):
    monkeypatch.setattr("src.core.limiter._trusted_nets", [])
    req = _request("192.0.2.7", {"X-Real-IP": "203.0.113.9"})
    assert _client_ip(req) == "192.0.2.7"
